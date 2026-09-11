package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
	"github.com/xuri/excelize/v2"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

var (
	// Track mutual groups processing per instance with contact info
	mutualGroupsProcessing     = make(map[string]map[string]string) // instanceID -> {"jid": "...", "name": "..."}
	mutualGroupsProcessingLock sync.Mutex
)

// ContactInfo represents contact information for list and export
type ContactInfo struct {
	JID         string `json:"jid"`
	PhoneNumber string `json:"phoneNumber"`
	Name        string `json:"name"`
	IsGroup     bool   `json:"isGroup"`
	IsLID       bool   `json:"isLID,omitempty"`
}

// CheckNumberRequest for checking if phone number is registered
type CheckNumberRequest struct {
	Phone string `json:"phone" validate:"required"`
}

// POST /check/:instanceId
func CheckNumber(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req CheckNumberRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	// Import helper package for phone number formatting
	recipient, err := helper.FormatPhoneNumber(req.Phone)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid phone number", "INVALID_PHONE", err.Error())
	}

	willSkipValidation := helper.ShouldSkipValidation(req.Phone)

	isRegistered, err := session.Client.IsOnWhatsApp(context.Background(), []string{recipient.User})
	if err != nil {
		return ErrorResponse(c, 500, "Failed to check phone number", "CHECK_FAILED", err.Error())
	}

	if len(isRegistered) == 0 {
		return ErrorResponse(c, 400, "Unable to verify number", "VERIFICATION_ERROR", "")
	}

	return SuccessResponse(c, 200, "Phone number checked", map[string]any{
		"phone":              req.Phone,
		"isRegistered":       isRegistered[0].IsIn,
		"jid":                isRegistered[0].JID.String(),
		"willSkipValidation": willSkipValidation,
		"note":               getValidationNote(isRegistered[0].IsIn, willSkipValidation),
	})
}

// Helper function to provide user-friendly note about validation behavior
func getValidationNote(isRegistered, willSkip bool) string {
	if willSkip {
		if isRegistered {
			return "Number is registered. Validation will be skipped when sending (ALLOW_9_DIGIT_PHONE_NUMBER=true)"
		}
		return "Number appears unregistered, but validation will be skipped when sending (ALLOW_9_DIGIT_PHONE_NUMBER=true). Message will be attempted anyway."
	}
	if isRegistered {
		return "Number is registered and will pass validation when sending"
	}
	return "Number is not registered. Message sending will be blocked unless ALLOW_9_DIGIT_PHONE_NUMBER=true is set"
}

// GET /contacts/:instanceId/:jid
func GetContactDetail(c echo.Context) error {
	instanceID := c.Param("instanceId")
	jidParam := c.Param("jid")

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	// Parse JID
	jid, err := types.ParseJID(jidParam)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid JID format", "INVALID_JID", err.Error())
	}

	// Get contact from store
	contact, err := session.Client.Store.Contacts.GetContact(context.Background(), jid)
	if err != nil {
		return ErrorResponse(c, 404, "Contact not found", "CONTACT_NOT_FOUND", err.Error())
	}

	contactDetail := map[string]any{
		"jid":            jid.String(),
		"phoneNumber":    jid.User,
		"name":           contactDisplayName(contact, jid.User),
		"businessName":   contact.BusinessName,
		"pushName":       contact.PushName,
		"profilePicture": "",
		"about":          "",
		"isGroup":        jid.Server == "g.us",
		"isBusiness":     contact.BusinessName != "",
		"verifiedName":   nil,
	}

	pic, err := session.Client.GetProfilePictureInfo(context.Background(), jid, &whatsmeow.GetProfilePictureParams{
		Preview: false,
	})
	if err == nil && pic != nil {
		contactDetail["profilePicture"] = pic.URL
	}

	// About status and verified-business info exist only for non-groups.
	if jid.Server != "g.us" {
		addContactUserInfo(session, jid, contactDetail)
	}

	return SuccessResponse(c, 200, "Contact details retrieved successfully", contactDetail)
}

// addContactUserInfo fills the about status and verified business name into the
// detail map. A lookup failure leaves the defaults in place.
func addContactUserInfo(session *model.Session, jid types.JID, contactDetail map[string]any) {
	userInfo, err := session.Client.GetUserInfo(context.Background(), []types.JID{jid})
	if err != nil || len(userInfo) == 0 {
		return
	}

	info := userInfo[jid]
	if info.Status != "" {
		contactDetail["about"] = info.Status
	}
	if info.VerifiedName != nil && info.VerifiedName.Details.GetVerifiedName() != "" {
		contactDetail["verifiedName"] = info.VerifiedName.Details.GetVerifiedName()
	}
}

// GET /contacts/:instanceId/:jid/mutual-groups
func GetMutualGroups(c echo.Context) error {
	instanceID := c.Param("instanceId")
	jidParam := c.Param("jid")

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	// Parse JID
	jid, err := types.ParseJID(jidParam)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid JID format", "INVALID_JID", err.Error())
	}

	// Get contact name for better error message
	contact, _ := session.Client.Store.Contacts.GetContact(context.Background(), jid)
	contactName := contactDisplayName(contact, jid.User)

	release, errResp := claimMutualGroupsSlot(c, instanceID, jid, contactName)
	if errResp != nil {
		return errResp
	}
	defer release()

	// A group JID has no mutual groups of its own.
	if jid.Server == "g.us" {
		return SuccessResponse(c, 200, "Mutual groups retrieved successfully", map[string]any{
			"jid":          jid.String(),
			"mutualGroups": []string{},
		})
	}

	groups, err := session.Client.GetJoinedGroups(context.Background())
	if err != nil {
		return ErrorResponse(c, 500, "Failed to get joined groups", "FETCH_FAILED", err.Error())
	}

	mutualGroups := collectMutualGroups(session, groups, jid.User)

	return SuccessResponse(c, 200, "Mutual groups retrieved successfully", map[string]any{
		"jid":          jid.String(),
		"mutualGroups": mutualGroups,
		"total":        len(mutualGroups),
	})
}

// claimMutualGroupsSlot reserves the single mutual-groups scan slot for an
// instance. The scan walks every joined group with a one-second pause, so only
// one may run at a time. The returned function releases the slot; the second
// result is a written ErrorResponse when the slot is already taken.
func claimMutualGroupsSlot(c echo.Context, instanceID string, jid types.JID, contactName string) (func(), error) {
	mutualGroupsProcessingLock.Lock()
	if processingInfo, exists := mutualGroupsProcessing[instanceID]; exists {
		mutualGroupsProcessingLock.Unlock()
		return nil, ErrorResponse(c, 409, "Mutual groups check already in progress", "ALREADY_PROCESSING",
			fmt.Sprintf("Currently checking mutual groups for: %s (%s). Please wait for it to complete.",
				processingInfo["name"], processingInfo["jid"]))
	}
	mutualGroupsProcessing[instanceID] = map[string]string{
		"jid":  jid.String(),
		"name": contactName,
	}
	mutualGroupsProcessingLock.Unlock()

	log.Printf("🔒 [Mutual Groups] Acquired lock for instance: %s (checking: %s)", instanceID, contactName)

	return func() {
		mutualGroupsProcessingLock.Lock()
		delete(mutualGroupsProcessing, instanceID)
		mutualGroupsProcessingLock.Unlock()
		log.Printf("🔓 [Mutual Groups] Released lock for instance: %s", instanceID)
	}, nil
}

// groupHasParticipant reports whether phoneNumber is a member of the group,
// resolving linked-device (@lid) participants to their phone number first.
func groupHasParticipant(session *model.Session, groupInfo *types.GroupInfo, phoneNumber string) bool {
	for _, participant := range groupInfo.Participants {
		participantPhone := participant.JID.User

		if participant.JID.Server == "lid" {
			phoneJID, err := session.Client.Store.LIDs.GetPNForLID(context.Background(), participant.JID)
			if err == nil && phoneJID.User != "" {
				participantPhone = phoneJID.User
			}
		}

		if participantPhone == phoneNumber {
			return true
		}
	}
	return false
}

// collectMutualGroups returns the names of the joined groups that phoneNumber is
// also in. Requests are paced one second apart to stay under WhatsApp's rate limit.
func collectMutualGroups(session *model.Session, groups []*types.GroupInfo, phoneNumber string) []string {
	mutualGroups := []string{}
	log.Printf("🔍 [Mutual Groups] Starting search for %s in %d groups", phoneNumber, len(groups))

	for i, group := range groups {
		if i > 0 {
			time.Sleep(1 * time.Second)
		}

		groupInfo, err := session.Client.GetGroupInfo(context.Background(), group.JID)
		if err != nil {
			log.Printf("⚠️ [%d/%d] Error getting group info: %v", i+1, len(groups), err)
			continue
		}

		log.Printf("📋 [%d/%d] Checking group: %s (%d participants)", i+1, len(groups), groupInfo.Name, len(groupInfo.Participants))
		if groupHasParticipant(session, groupInfo, phoneNumber) {
			mutualGroups = append(mutualGroups, groupInfo.Name)
			log.Printf("✅ [%d/%d] Found match in: %s", i+1, len(groups), groupInfo.Name)
		}
	}

	log.Printf("🎉 [Mutual Groups] Search complete! Found %d mutual groups", len(mutualGroups))
	return mutualGroups
}

// contactDisplayName returns the contact's best available name, falling back to
// the business name, then the push name, then the bare phone number.
func contactDisplayName(contact types.ContactInfo, fallback string) string {
	if contact.FullName != "" {
		return contact.FullName
	}
	if contact.BusinessName != "" {
		return contact.BusinessName
	}
	if contact.PushName != "" {
		return contact.PushName
	}
	return fallback
}

// queryPositiveInt reads a positive integer query param, or returns defaultVal.
func queryPositiveInt(c echo.Context, name string, defaultVal int) int {
	value, err := strconv.Atoi(c.QueryParam(name))
	if err != nil || value <= 0 {
		return defaultVal
	}
	return value
}

// buildContactMap deduplicates the store's contacts by phone number, dropping
// linked-device (@lid) entries and preferring the entry with a real name.
func buildContactMap(contacts map[types.JID]types.ContactInfo) map[string]ContactInfo {
	contactMap := make(map[string]ContactInfo, len(contacts))

	for jid, contact := range contacts {
		// Skip all LID contacts (linked devices)
		if jid.Server == "lid" {
			continue
		}

		contactInfo := ContactInfo{
			JID:         jid.String(),
			PhoneNumber: jid.User,
			Name:        contactDisplayName(contact, jid.User),
			IsGroup:     jid.Server == "g.us",
		}

		// Later entries only win when they carry a real name rather than the
		// phone number repeated back.
		_, exists := contactMap[contactInfo.PhoneNumber]
		if !exists || contactInfo.Name != contactInfo.PhoneNumber {
			contactMap[contactInfo.PhoneNumber] = contactInfo
		}
	}

	return contactMap
}

// matchesContactSearch reports whether a contact matches the lowercased query
// on its name, JID or phone number. An empty query matches everything.
func matchesContactSearch(contactInfo ContactInfo, searchQuery string) bool {
	if searchQuery == "" {
		return true
	}
	return strings.Contains(strings.ToLower(contactInfo.Name), searchQuery) ||
		strings.Contains(strings.ToLower(contactInfo.JID), searchQuery) ||
		strings.Contains(strings.ToLower(contactInfo.PhoneNumber), searchQuery)
}

// paginateContacts returns the requested slice of contacts. An out-of-range
// page yields an empty slice rather than an error.
func paginateContacts(contacts []ContactInfo, page, limit int) []ContactInfo {
	startIndex := (page - 1) * limit
	if startIndex >= len(contacts) {
		return []ContactInfo{}
	}
	return contacts[startIndex:min(startIndex+limit, len(contacts))]
}

// GET /contacts/:instanceId?page=1&limit=50&search=john
func GetContactList(c echo.Context) error {
	instanceID := c.Param("instanceId")

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	page := queryPositiveInt(c, "page", 1)
	limit := min(queryPositiveInt(c, "limit", 50), 50)
	searchQuery := strings.ToLower(strings.TrimSpace(c.QueryParam("search")))

	contacts, err := session.Client.Store.Contacts.GetAllContacts(context.Background())
	if err != nil {
		return ErrorResponse(c, 500, "Failed to retrieve contact list", "FETCH_FAILED", err.Error())
	}

	allContacts := make([]ContactInfo, 0, len(contacts))
	for _, contactInfo := range buildContactMap(contacts) {
		if matchesContactSearch(contactInfo, searchQuery) {
			allContacts = append(allContacts, contactInfo)
		}
	}

	totalContacts := len(allContacts)
	totalPages := (totalContacts + limit - 1) / limit
	paginatedContacts := paginateContacts(allContacts, page, limit)

	return SuccessResponse(c, 200, "Contact list retrieved successfully", map[string]any{
		"total":       totalContacts,
		"page":        page,
		"limit":       limit,
		"totalPages":  totalPages,
		"search":      searchQuery,
		"contacts":    paginatedContacts,
		"hasNextPage": page < totalPages,
		"hasPrevPage": page > 1,
	})
}

// GET /contacts/:instanceId/export?format=xlsx
func ExportContacts(c echo.Context) error {
	instanceID := c.Param("instanceId")
	format := c.QueryParam("format")

	// Default to xlsx if not specified
	if format == "" {
		format = "xlsx"
	}

	// Validate format
	if format != "xlsx" && format != "csv" {
		return ErrorResponse(c, 400, "Invalid format", "INVALID_FORMAT", "Format must be 'xlsx' or 'csv'")
	}

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	contacts, err := session.Client.Store.Contacts.GetAllContacts(context.Background())
	if err != nil {
		return ErrorResponse(c, 500, "Failed to retrieve contact list", "FETCH_FAILED", err.Error())
	}

	contactMap := buildContactMap(contacts)
	allContacts := make([]ContactInfo, 0, len(contactMap))
	for _, contact := range contactMap {
		allContacts = append(allContacts, contact)
	}

	if format == "xlsx" {
		return exportToExcel(c, allContacts, instanceID)
	}
	return exportToCSV(c, allContacts, instanceID)
}

func exportToExcel(c echo.Context, contacts []ContactInfo, instanceID string) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheetName := "Contacts"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to create Excel sheet", "EXCEL_ERROR", err.Error())
	}

	if err := writeContactSheet(f, sheetName, contacts); err != nil {
		return ErrorResponse(c, 500, "Failed to build Excel sheet", "EXCEL_ERROR", err.Error())
	}

	f.SetActiveSheet(index)
	if err := f.DeleteSheet("Sheet1"); err != nil {
		return ErrorResponse(c, 500, "Failed to build Excel sheet", "EXCEL_ERROR", err.Error())
	}

	// Set response headers
	filename := fmt.Sprintf("contacts_%s.xlsx", instanceID)
	c.Response().Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	return f.Write(c.Response().Writer)
}

// contactSheetColumns lists the export columns with their header text and width.
var contactSheetColumns = []struct {
	letter string
	header string
	width  float64
}{
	{"A", "No", 5},
	{"B", "Phone Number", 15},
	{"C", "Name", 25},
	{"D", "JID", 35},
	{"E", "Type", 10},
}

// writeContactSheet fills one sheet with the header row, the contact rows and
// the column widths. It stops at the first excelize error.
func writeContactSheet(f *excelize.File, sheetName string, contacts []ContactInfo) error {
	if err := writeContactHeader(f, sheetName); err != nil {
		return err
	}

	for i, contact := range contacts {
		if err := writeContactRow(f, sheetName, i, contact); err != nil {
			return err
		}
	}

	for _, col := range contactSheetColumns {
		if err := f.SetColWidth(sheetName, col.letter, col.letter, col.width); err != nil {
			return err
		}
	}

	return nil
}

// writeContactHeader writes the styled header row.
func writeContactHeader(f *excelize.File, sheetName string) error {
	for _, col := range contactSheetColumns {
		if err := f.SetCellValue(sheetName, col.letter+"1", col.header); err != nil {
			return err
		}
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return err
	}

	return f.SetCellStyle(sheetName, "A1", "E1", headerStyle)
}

// writeContactRow writes one contact. index is zero-based; row 1 holds the header.
func writeContactRow(f *excelize.File, sheetName string, index int, contact ContactInfo) error {
	contactType := "Contact"
	if contact.IsGroup {
		contactType = "Group"
	}

	row := index + 2
	values := []any{
		index + 1,
		sanitizeCell(contact.PhoneNumber),
		sanitizeCell(contact.Name),
		sanitizeCell(contact.JID),
		contactType,
	}

	for i, col := range contactSheetColumns {
		cell := fmt.Sprintf("%s%d", col.letter, row)
		if err := f.SetCellValue(sheetName, cell, values[i]); err != nil {
			return err
		}
	}

	return nil
}

// sanitizeCell prefixes values starting with spreadsheet-formula characters with
// a single quote so CSV/XLSX viewers treat them as literal text instead of
// executing them as formulas. This blocks CSV injection via malicious contact
// names (CVE class: "CWE-1236: Improper Neutralization of Formula Elements").
func sanitizeCell(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

func exportToCSV(c echo.Context, contacts []ContactInfo, instanceID string) error {
	c.Response().Header().Set("Content-Type", "text/csv")
	filename := fmt.Sprintf("contacts_%s.csv", instanceID)
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	writer := csv.NewWriter(c.Response().Writer)
	defer writer.Flush()

	// Write headers
	headers := []string{"No", "Phone Number", "Name", "JID", "Type"}
	if err := writer.Write(headers); err != nil {
		return ErrorResponse(c, 500, "Failed to write CSV headers", "CSV_ERROR", err.Error())
	}

	// Write data
	for i, contact := range contacts {
		contactType := "Contact"
		if contact.IsGroup {
			contactType = "Group"
		}

		row := []string{
			strconv.Itoa(i + 1),
			sanitizeCell(contact.PhoneNumber),
			sanitizeCell(contact.Name),
			sanitizeCell(contact.JID),
			contactType,
		}

		if err := writer.Write(row); err != nil {
			return ErrorResponse(c, 500, "Failed to write CSV row", "CSV_ERROR", err.Error())
		}
	}

	return nil
}

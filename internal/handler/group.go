package handler

import (
	"context"
	"errors"
	"fmt"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// Request for sending a group message
type SendGroupMessageRequest struct {
	GroupJID string `json:"groupJid" validate:"required"`
	Message  string `json:"message" validate:"required"`
}

// GET /groups/:instanceId - List all groups
func GetGroups(c echo.Context) error {
	instanceID := c.Param("instanceId")

	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	// Get all groups with context
	groups, err := session.Client.GetJoinedGroups(context.Background())
	if err != nil {
		return ErrorResponse(c, 500, "Failed to get groups", "GET_GROUPS_FAILED", err.Error())
	}

	groupList := make([]map[string]interface{}, 0)
	for _, groupInfo := range groups {
		groupList = append(groupList, map[string]interface{}{
			"jid":          groupInfo.JID.String(),
			"name":         groupInfo.Name,
			"topic":        groupInfo.Topic,
			"participants": len(groupInfo.Participants),
			"ownerJid":     groupInfo.OwnerJID.String(),
			"createdAt":    groupInfo.GroupCreated.Unix(),
		})
	}

	return SuccessResponse(c, 200, "Groups retrieved", map[string]interface{}{
		"total":  len(groupList),
		"groups": groupList,
	})
}

// POST /send-group/:instanceId - Send text to group
func SendGroupMessage(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req SendGroupMessageRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.GroupJID == "" || req.Message == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'message' are required", "VALIDATION_ERROR", "")
	}

	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "")
	}

	// Parse group JID
	groupJID, err := types.ParseJID(req.GroupJID)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid group JID", "INVALID_GROUP_JID", err.Error())
	}

	// Validate it's a group JID (ends with @g.us)
	if groupJID.Server != types.GroupServer {
		return ErrorResponse(c, 400, "Not a group JID", "NOT_GROUP_JID", "Group JID must end with @g.us")
	}

	// Typing delay simulation
	messageLength := len(req.Message)
	helper.ApplyTypingDelay(session.Client, groupJID, messageLength)

	// Create message
	msg := &waE2E.Message{
		Conversation: &req.Message,
	}

	// Send to group
	resp, err := session.Client.SendMessage(context.Background(), groupJID, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send message", "SEND_FAILED", err.Error())
	}

	return SuccessResponse(c, 200, "Message sent to group", map[string]interface{}{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
	})
}

// POST /send-group/:instanceId/media - Send media to group
func SendGroupMedia(c echo.Context) error {
	instanceID := c.Param("instanceId")

	groupJid := c.FormValue("groupJid")
	caption := c.FormValue("caption")

	if groupJid == "" {
		return ErrorResponse(c, 400, "Field 'groupJid' is required", "VALIDATION_ERROR", "")
	}

	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "")
	}

	// Parse group JID
	groupJID, err := types.ParseJID(groupJid)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid group JID", "INVALID_GROUP_JID", err.Error())
	}

	if groupJID.Server != types.GroupServer {
		return ErrorResponse(c, 400, "Not a group JID", "NOT_GROUP_JID", "Group JID must end with @g.us")
	}

	// Get file
	file, err := c.FormFile("file")
	if err != nil {
		return ErrorResponse(c, 400, "File is required", "FILE_REQUIRED", err.Error())
	}

	mediaType := helper.DetectMediaType(file.Filename)

	maxSize := getMaxFileSize(mediaType)
	fileData, err := readMultipartUpload(file, maxSize)
	if err != nil {
		return ErrorResponse(c, 400, "File too large or unreadable", "FILE_TOO_LARGE",
			fmt.Sprintf("Type: %s, Max: %d bytes, Error: %v", mediaType, maxSize, err))
	}

	// Verify content type by magic-byte sniffing — reject spoofed extensions.
	sniffedType := helper.DetectMediaTypeFromBytes(fileData)
	if sniffedType != mediaType {
		sniffedMax := getMaxFileSize(sniffedType)
		if len(fileData) > sniffedMax {
			return ErrorResponse(c, 400, "File too large for detected media type", "FILE_TOO_LARGE",
				fmt.Sprintf("Detected: %s, Max: %d bytes, Got: %d", sniffedType, sniffedMax, len(fileData)))
		}
		mediaType = sniffedType
	}

	var whatsmeowMediaType whatsmeow.MediaType
	switch mediaType {
	case "image":
		whatsmeowMediaType = whatsmeow.MediaImage
	case "video":
		whatsmeowMediaType = whatsmeow.MediaVideo
	case "audio":
		whatsmeowMediaType = whatsmeow.MediaAudio
	default:
		whatsmeowMediaType = whatsmeow.MediaDocument
	}

	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media", "UPLOAD_FAILED", err.Error())
	}

	// Typing delay simulation
	messageLength := len(caption)
	helper.ApplyTypingDelay(session.Client, groupJID, messageLength)

	msg := helper.CreateMediaMessage(uploaded, caption, file.Filename, mediaType)

	resp, err := session.Client.SendMessage(context.Background(), groupJID, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]interface{}{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  groupJid,
		"mediaType": mediaType,
		"fileName":  file.Filename,
		"fileSize":  len(fileData),
	})
}

// POST /send-group/:instanceId/media-url - Send media from URL to group
func SendGroupMediaURL(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req struct {
		GroupJID  string `json:"groupJid" validate:"required"`
		MediaURL  string `json:"mediaUrl" validate:"required"`
		Caption   string `json:"caption"`
		MediaType string `json:"mediaType"`
	}

	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.GroupJID == "" || req.MediaURL == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'mediaUrl' are required", "VALIDATION_ERROR", "")
	}

	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "")
	}

	groupJID, err := types.ParseJID(req.GroupJID)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid group JID", "INVALID_GROUP_JID", err.Error())
	}

	if groupJID.Server != types.GroupServer {
		return ErrorResponse(c, 400, "Not a group JID", "NOT_GROUP_JID", "Group JID must end with @g.us")
	}

	fileData, filename, err := helper.DownloadFile(req.MediaURL)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to download file", "DOWNLOAD_FAILED", err.Error())
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = helper.DetectMediaType(filename)
	}

	maxSize := getMaxFileSize(mediaType)
	if len(fileData) > maxSize {
		return ErrorResponse(c, 400, "File too large", "FILE_TOO_LARGE",
			fmt.Sprintf("File: %d bytes, Max: %d bytes", len(fileData), maxSize))
	}

	var whatsmeowMediaType whatsmeow.MediaType
	switch mediaType {
	case "image":
		whatsmeowMediaType = whatsmeow.MediaImage
	case "video":
		whatsmeowMediaType = whatsmeow.MediaVideo
	case "audio":
		whatsmeowMediaType = whatsmeow.MediaAudio
	default:
		whatsmeowMediaType = whatsmeow.MediaDocument
	}

	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media", "UPLOAD_FAILED", err.Error())
	}

	// Typing delay simulation
	messageLength := len(req.Caption)
	helper.ApplyTypingDelay(session.Client, groupJID, messageLength)

	msg := helper.CreateMediaMessage(uploaded, req.Caption, filename, mediaType)

	resp, err := session.Client.SendMessage(context.Background(), groupJID, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]interface{}{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
	})
}

// GET /groups/by-number/:phoneNumber - List all groups
func GetGroupsByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveInstance) {
			return ErrorResponse(c, 404, "No active instance for this phone number", "NO_ACTIVE_INSTANCE", "Please login / scan QR for this number")
		}
		return ErrorResponse(c, 500, "Failed to get instance for this phone number", "DB_ERROR", err.Error())
	}

	// Permission Check
	userClaims, _ := c.Get("user_claims").(*service.Claims)
	if userClaims != nil && userClaims.Role != "admin" {
		_, err := model.CheckUserInstancePermission(userClaims.UserID, inst.InstanceID)
		if err != nil {
			return ErrorResponse(c, 403, "Insufficient permission to use this phone number", "FORBIDDEN", "")
		}
	}

	session, err := service.GetSession(inst.InstanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	groups, err := session.Client.GetJoinedGroups(context.Background())
	if err != nil {
		return ErrorResponse(c, 500, "Failed to get groups", "GET_GROUPS_FAILED", err.Error())
	}

	groupList := make([]map[string]interface{}, 0)
	for _, groupInfo := range groups {
		groupList = append(groupList, map[string]interface{}{
			"jid":          groupInfo.JID.String(),
			"name":         groupInfo.Name,
			"topic":        groupInfo.Topic,
			"participants": len(groupInfo.Participants),
			"ownerJid":     groupInfo.OwnerJID.String(),
			"createdAt":    groupInfo.GroupCreated.Unix(),
		})
	}

	return SuccessResponse(c, 200, "Groups retrieved", map[string]interface{}{
		"from":   phoneNumber,
		"total":  len(groupList),
		"groups": groupList,
	})
}

// POST /send-group/by-number/:phoneNumber - Send text to group by sender number
func SendGroupMessageByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	var req SendGroupMessageRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.GroupJID == "" || req.Message == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'message' are required", "VALIDATION_ERROR", "")
	}

	// 1. Find active instance by sender number
	inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveInstance) {
			return ErrorResponse(c, 404, "No active instance for this phone number", "NO_ACTIVE_INSTANCE", "Please login / scan QR for this number")
		}
		return ErrorResponse(c, 500, "Failed to get instance for this phone number", "DB_ERROR", err.Error())
	}

	// Permission Check
	userClaims, _ := c.Get("user_claims").(*service.Claims)
	if userClaims != nil && userClaims.Role != "admin" {
		_, err := model.CheckUserInstancePermission(userClaims.UserID, inst.InstanceID)
		if err != nil {
			return ErrorResponse(c, 403, "Insufficient permission to use this phone number", "FORBIDDEN", "")
		}
	}

	// 2. Get session from memory by instance_id
	session, err := service.GetSession(inst.InstanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "")
	}

	// Parse group JID
	groupJID, err := types.ParseJID(req.GroupJID)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid group JID", "INVALID_GROUP_JID", err.Error())
	}

	// Validate it's a group JID (ends with @g.us)
	if groupJID.Server != types.GroupServer {
		return ErrorResponse(c, 400, "Not a group JID", "NOT_GROUP_JID", "Group JID must end with @g.us")
	}

	// Typing delay simulation
	messageLength := len(req.Message)
	helper.ApplyTypingDelay(session.Client, groupJID, messageLength)

	// Create message
	msg := &waE2E.Message{
		Conversation: &req.Message,
	}

	// Send to group
	resp, err := session.Client.SendMessage(context.Background(), groupJID, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send message", "SEND_FAILED", err.Error())
	}

	return SuccessResponse(c, 200, "Message sent to group", map[string]interface{}{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
	})
}

// POST /send-group/by-number/:phoneNumber/media - Send media to group by sender number
func SendGroupMediaByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	groupJid := c.FormValue("groupJid")
	caption := c.FormValue("caption")

	if groupJid == "" {
		return ErrorResponse(c, 400, "Field 'groupJid' is required", "VALIDATION_ERROR", "")
	}

	// 1. Find active instance by sender number
	inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveInstance) {
			return ErrorResponse(c, 404, "No active instance for this phone number", "NO_ACTIVE_INSTANCE", "Please login / scan QR for this number")
		}
		return ErrorResponse(c, 500, "Failed to get instance for this phone number", "DB_ERROR", err.Error())
	}

	// Permission Check
	userClaims, _ := c.Get("user_claims").(*service.Claims)
	if userClaims != nil && userClaims.Role != "admin" {
		_, err := model.CheckUserInstancePermission(userClaims.UserID, inst.InstanceID)
		if err != nil {
			return ErrorResponse(c, 403, "Insufficient permission to use this phone number", "FORBIDDEN", "")
		}
	}

	// 2. Get session from memory by instance_id
	session, err := service.GetSession(inst.InstanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "")
	}

	// Parse group JID
	groupJID, err := types.ParseJID(groupJid)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid group JID", "INVALID_GROUP_JID", err.Error())
	}

	if groupJID.Server != types.GroupServer {
		return ErrorResponse(c, 400, "Not a group JID", "NOT_GROUP_JID", "Group JID must end with @g.us")
	}

	// Get file
	file, err := c.FormFile("file")
	if err != nil {
		return ErrorResponse(c, 400, "File is required", "FILE_REQUIRED", err.Error())
	}

	mediaType := helper.DetectMediaType(file.Filename)

	maxSize := getMaxFileSize(mediaType)
	fileData, err := readMultipartUpload(file, maxSize)
	if err != nil {
		return ErrorResponse(c, 400, "File too large or unreadable", "FILE_TOO_LARGE",
			fmt.Sprintf("Type: %s, Max: %d bytes, Error: %v", mediaType, maxSize, err))
	}

	// Verify content type by magic-byte sniffing — reject spoofed extensions.
	sniffedType := helper.DetectMediaTypeFromBytes(fileData)
	if sniffedType != mediaType {
		sniffedMax := getMaxFileSize(sniffedType)
		if len(fileData) > sniffedMax {
			return ErrorResponse(c, 400, "File too large for detected media type", "FILE_TOO_LARGE",
				fmt.Sprintf("Detected: %s, Max: %d bytes, Got: %d", sniffedType, sniffedMax, len(fileData)))
		}
		mediaType = sniffedType
	}

	var whatsmeowMediaType whatsmeow.MediaType
	switch mediaType {
	case "image":
		whatsmeowMediaType = whatsmeow.MediaImage
	case "video":
		whatsmeowMediaType = whatsmeow.MediaVideo
	case "audio":
		whatsmeowMediaType = whatsmeow.MediaAudio
	default:
		whatsmeowMediaType = whatsmeow.MediaDocument
	}

	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media", "UPLOAD_FAILED", err.Error())
	}

	// Typing delay simulation
	messageLength := len(caption)
	helper.ApplyTypingDelay(session.Client, groupJID, messageLength)

	msg := helper.CreateMediaMessage(uploaded, caption, file.Filename, mediaType)

	resp, err := session.Client.SendMessage(context.Background(), groupJID, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]interface{}{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  groupJid,
		"mediaType": mediaType,
		"fileName":  file.Filename,
		"fileSize":  len(fileData),
	})
}

// POST /send-group/by-number/:phoneNumber/media-url - Send media from URL to group by sender number
func SendGroupMediaURLByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	var req struct {
		GroupJID  string `json:"groupJid" validate:"required"`
		MediaURL  string `json:"mediaUrl" validate:"required"`
		Caption   string `json:"caption"`
		MediaType string `json:"mediaType"`
	}

	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.GroupJID == "" || req.MediaURL == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'mediaUrl' are required", "VALIDATION_ERROR", "")
	}

	// 1. Find active instance by sender number
	inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveInstance) {
			return ErrorResponse(c, 404, "No active instance for this phone number", "NO_ACTIVE_INSTANCE", "Please login / scan QR for this number")
		}
		return ErrorResponse(c, 500, "Failed to get instance for this phone number", "DB_ERROR", err.Error())
	}

	// Permission Check
	userClaims, _ := c.Get("user_claims").(*service.Claims)
	if userClaims != nil && userClaims.Role != "admin" {
		_, err := model.CheckUserInstancePermission(userClaims.UserID, inst.InstanceID)
		if err != nil {
			return ErrorResponse(c, 403, "Insufficient permission to use this phone number", "FORBIDDEN", "")
		}
	}

	// 2. Get session from memory by instance_id
	session, err := service.GetSession(inst.InstanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "")
	}

	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "")
	}

	groupJID, err := types.ParseJID(req.GroupJID)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid group JID", "INVALID_GROUP_JID", err.Error())
	}

	if groupJID.Server != types.GroupServer {
		return ErrorResponse(c, 400, "Not a group JID", "NOT_GROUP_JID", "Group JID must end with @g.us")
	}

	fileData, filename, err := helper.DownloadFile(req.MediaURL)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to download file", "DOWNLOAD_FAILED", err.Error())
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = helper.DetectMediaType(filename)
	}

	maxSize := getMaxFileSize(mediaType)
	if len(fileData) > maxSize {
		return ErrorResponse(c, 400, "File too large", "FILE_TOO_LARGE", fmt.Sprintf("File: %d bytes, Max: %d bytes", len(fileData), maxSize))
	}

	var whatsmeowMediaType whatsmeow.MediaType
	switch mediaType {
	case "image":
		whatsmeowMediaType = whatsmeow.MediaImage
	case "video":
		whatsmeowMediaType = whatsmeow.MediaVideo
	case "audio":
		whatsmeowMediaType = whatsmeow.MediaAudio
	default:
		whatsmeowMediaType = whatsmeow.MediaDocument
	}

	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media", "UPLOAD_FAILED", err.Error())
	}

	// Typing delay simulation
	messageLength := len(req.Caption)
	helper.ApplyTypingDelay(session.Client, groupJID, messageLength)

	msg := helper.CreateMediaMessage(uploaded, req.Caption, filename, mediaType)

	resp, err := session.Client.SendMessage(context.Background(), groupJID, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]interface{}{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
	})
}

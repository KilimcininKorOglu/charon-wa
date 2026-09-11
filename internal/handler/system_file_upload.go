// internal/handler/system_file_upload.go
package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// SystemDir is the directory for system-wide images
const SystemDir = "./uploads/system"

// systemIdentityTextFields maps each form field to the identity field it sets.
// An empty value leaves the stored value untouched.
var systemIdentityTextFields = []struct {
	formKey string
	target  func(*model.SystemIdentity) *string
}{
	{"company_name", func(i *model.SystemIdentity) *string { return &i.CompanyName }},
	{"company_short_name", func(i *model.SystemIdentity) *string { return &i.CompanyShortName }},
	{"company_description", func(i *model.SystemIdentity) *string { return &i.CompanyDescription }},
	{"company_address", func(i *model.SystemIdentity) *string { return &i.CompanyAddress }},
	{"company_phone", func(i *model.SystemIdentity) *string { return &i.CompanyPhone }},
	{"company_email", func(i *model.SystemIdentity) *string { return &i.CompanyEmail }},
	{"company_website", func(i *model.SystemIdentity) *string { return &i.CompanyWebsite }},
}

// systemImageKeys are the three brand images the identity form can replace.
var systemImageKeys = []string{"logo", "ico", "second_logo"}

// applySystemIdentityText copies the non-empty text fields from the form.
func applySystemIdentityText(c echo.Context, identity *model.SystemIdentity) {
	for _, field := range systemIdentityTextFields {
		if val := c.FormValue(field.formKey); val != "" {
			*field.target(identity) = val
		}
	}
}

// systemImageURL returns a pointer to the identity field holding the given
// image's URL, or nil when the key is not a brand image.
func systemImageURL(identity *model.SystemIdentity, key string) *string {
	switch key {
	case "logo":
		return &identity.LogoURL
	case "ico":
		return &identity.IcoURL
	case "second_logo":
		return &identity.SecondLogoURL
	default:
		return nil
	}
}

// removeReplacedSystemImage deletes the image a new upload replaces. The path
// is resolved and confined to the uploads tree before anything is removed.
func removeReplacedSystemImage(key, oldPath string) {
	if oldPath == "" {
		return
	}

	uploadsBase, err := filepath.Abs("uploads")
	if err != nil {
		log.Printf("⚠️ Failed to resolve uploads directory: %v", err)
		return
	}

	resolvedOld, err := filepath.Abs(filepath.Join(".", oldPath))
	if err != nil || !strings.HasPrefix(resolvedOld, uploadsBase+string(filepath.Separator)) {
		return
	}

	// A failure here leaves an orphaned file on disk, so report it.
	if delErr := helper.DeleteFile(resolvedOld); delErr != nil {
		log.Printf("⚠️ Failed to remove replaced %s image: %v", key, delErr)
	}
}

// processSystemImage validates, compresses and stores one uploaded brand image,
// then points the identity at the new file. It returns a written ErrorResponse
// on failure, or nil when the field carried no file.
func processSystemImage(c echo.Context, identity *model.SystemIdentity, key string) error {
	file, err := c.FormFile(key)
	if err != nil {
		if err == http.ErrMissingFile {
			return nil // This specific file wasn't uploaded
		}
		return ErrorResponse(c, http.StatusBadRequest,
			fmt.Sprintf("Error reading file %s", key), "INVALID_FILE", err.Error())
	}

	if err := helper.ValidateImageFile(file); err != nil {
		return ErrorResponse(c, http.StatusBadRequest,
			fmt.Sprintf("File %s is not a valid image", key), "INVALID_FILE", err.Error())
	}

	src, err := file.Open()
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError,
			fmt.Sprintf("Failed to open file %s", key), "FILE_OPEN_ERROR", err.Error())
	}

	if err := helper.CheckMagicBytes(src); err != nil {
		_ = src.Close()
		return ErrorResponse(c, http.StatusBadRequest,
			fmt.Sprintf("File %s is not a valid image", key), "INVALID_FILE_SIGNATURE", err.Error())
	}

	compressedData, err := helper.CompressAndResize(src, file)
	_ = src.Close()
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest,
			fmt.Sprintf("Image processing failed for %s", key), "PROCESSING_FAILED", err.Error())
	}

	if err := os.MkdirAll(SystemDir, 0750); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError,
			"Failed to create system upload directory", "DIRECTORY_ERROR", err.Error())
	}

	// Fixed filename per key, so a new upload overwrites the old file.
	filename := fmt.Sprintf("%s.webp", key)
	filePath := filepath.Join(SystemDir, filename)

	target := systemImageURL(identity, key)
	oldPath := *target
	*target = fmt.Sprintf("/uploads/system/%s", filename)
	removeReplacedSystemImage(key, oldPath)

	if err := os.WriteFile(filePath, compressedData, 0600); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError,
			fmt.Sprintf("Failed to save file %s", key), "FILE_WRITE_ERROR", err.Error())
	}

	return nil
}

// UpdateSystemIdentityFull handles both text settings and file uploads in one request (Admin Only)
// POST /api/system/identity
func UpdateSystemIdentityFull(c echo.Context) error {
	userClaims, ok := c.Get("user_claims").(*service.Claims)
	if !ok || userClaims.Role != "admin" {
		return ErrorResponse(c, http.StatusForbidden, "Admin access required", "FORBIDDEN", "")
	}

	identity, err := model.GetSystemIdentity()
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError,
			"Failed to fetch current settings", "DB_ERROR", err.Error())
	}

	applySystemIdentityText(c, identity)

	for _, key := range systemImageKeys {
		if errResp := processSystemImage(c, identity, key); errResp != nil {
			return errResp
		}
	}

	if err := model.UpdateSystemIdentitySettings(identity); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError,
			"Failed to save settings to database", "DB_ERROR", err.Error())
	}

	_ = model.LogAction(&model.AuditLog{
		UserID:       sql.NullInt64{Int64: userClaims.UserID, Valid: true},
		Action:       "system.identity.update_full",
		ResourceType: sql.NullString{String: "system", Valid: true},
		ResourceID:   sql.NullString{String: "identity", Valid: true},
		Details: map[string]any{
			"updated_by": userClaims.Username,
			"timestamp":  helper.GetTimestamp(),
		},
		IPAddress: sql.NullString{String: c.RealIP(), Valid: true},
		UserAgent: sql.NullString{String: c.Request().UserAgent(), Valid: true},
	})

	return SuccessResponse(c, http.StatusOK, "System identity updated successfully", identity)
}

// GetSystemIdentityHandler returns the global identity settings
// GET /api/system/identity
func GetSystemIdentityHandler(c echo.Context) error {
	identity, err := model.GetSystemIdentity()
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError,
			"Failed to get system identity", "DB_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "System identity retrieved", identity)
}

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

// UpdateSystemIdentityFull handles both text settings and file uploads in one request (Admin Only)
// POST /api/system/identity
func UpdateSystemIdentityFull(c echo.Context) error {
	userClaims, ok := c.Get("user_claims").(*service.Claims)
	if !ok || userClaims.Role != "admin" {
		return c.JSON(http.StatusForbidden, map[string]any{
			"success": false,
			"message": "Admin access required",
		})
	}

	// 1. Get current identity from DB
	identity, err := model.GetSystemIdentity()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to fetch current settings",
		})
	}

	// 2. Map text fields from form-data (Only update if provided/not empty)
	if val := c.FormValue("company_name"); val != "" {
		identity.CompanyName = val
	}
	if val := c.FormValue("company_short_name"); val != "" {
		identity.CompanyShortName = val
	}
	if val := c.FormValue("company_description"); val != "" {
		identity.CompanyDescription = val
	}
	if val := c.FormValue("company_address"); val != "" {
		identity.CompanyAddress = val
	}
	if val := c.FormValue("company_phone"); val != "" {
		identity.CompanyPhone = val
	}
	if val := c.FormValue("company_email"); val != "" {
		identity.CompanyEmail = val
	}
	if val := c.FormValue("company_website"); val != "" {
		identity.CompanyWebsite = val
	}

	// 3. Handle optional file uploads (logo, ico, second_logo)
	fileKeys := []string{"logo", "ico", "second_logo"}
	for _, key := range fileKeys {
		file, err := c.FormFile(key)
		if err != nil {
			if err == http.ErrMissingFile {
				continue // Skip if this specific file wasn't uploaded
			}
			return c.JSON(http.StatusBadRequest, map[string]any{
				"success": false,
				"message": fmt.Sprintf("Error reading file %s", key),
			})
		}

		// Validate and Process File
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

		// Create system directory
		if err := os.MkdirAll(SystemDir, 0750); err != nil {
			return ErrorResponse(c, http.StatusInternalServerError,
				"Failed to create system upload directory", "DIRECTORY_ERROR", err.Error())
		}

		// Use fixed filename (will overwrite old file)
		filename := fmt.Sprintf("%s.webp", key)
		filePath := filepath.Join(SystemDir, filename)
		imageURL := fmt.Sprintf("/uploads/system/%s", filename)

		// Delete old file if exists (will be overwritten anyway)
		var oldPath string
		switch key {
		case "logo":
			oldPath = identity.LogoURL
			identity.LogoURL = imageURL
		case "ico":
			oldPath = identity.IcoURL
			identity.IcoURL = imageURL
		case "second_logo":
			oldPath = identity.SecondLogoURL
			identity.SecondLogoURL = imageURL
		}

		if oldPath != "" {
			uploadsBase, _ := filepath.Abs("uploads")
			resolvedOld, err := filepath.Abs(filepath.Join(".", oldPath))
			if err == nil && strings.HasPrefix(resolvedOld, uploadsBase+string(filepath.Separator)) {
				// A failure here leaves an orphaned file on disk, so report it.
				if delErr := helper.DeleteFile(resolvedOld); delErr != nil {
					log.Printf("⚠️ Failed to remove replaced %s image: %v", key, delErr)
				}
			}
		}

		// Save new file
		if err := os.WriteFile(filePath, compressedData, 0600); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"success": false,
				"message": fmt.Sprintf("Failed to save file %s", key),
			})
		}
	}

	// 4. Save everything back to database using existing model logic
	if err := model.UpdateSystemIdentitySettings(identity); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to save settings to database",
		})
	}

	// 5. Log audit
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

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "System identity updated successfully",
		"data":    identity,
	})
}

// GetSystemIdentityHandler returns the global identity settings
// GET /api/system/identity
func GetSystemIdentityHandler(c echo.Context) error {
	identity, err := model.GetSystemIdentity()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to get system identity",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data":    identity,
	})
}

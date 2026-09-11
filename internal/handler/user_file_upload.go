// internal/handler/file_upload.go
package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// UploadAvatar handles avatar upload
// POST /api/me/avatar
func UploadAvatar(c echo.Context) error {
	// Get user from context
	userClaims, ok := c.Get("user_claims").(*service.Claims)
	if !ok {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	// Get uploaded file
	file, err := c.FormFile("avatar")
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "No file uploaded", "NO_FILE", err.Error())
	}

	// Validate file (basic validation)
	if err := helper.ValidateImageFile(file); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "File is not a valid image", "INVALID_FILE", err.Error())
	}

	// Open file
	src, err := file.Open()
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to open uploaded file", "FILE_OPEN_ERROR", err.Error())
	}
	defer src.Close()

	// Check magic bytes (file signature validation)
	if err := helper.CheckMagicBytes(src); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "File is not a valid image", "INVALID_FILE_SIGNATURE", err.Error())
	}

	// Process image: validate, compress, convert to WebP
	log.Printf("📸 Processing avatar upload for user %d (original size: %d bytes)", userClaims.UserID, file.Size)

	compressedData, err := helper.CompressAndResize(src, file)
	if err != nil {
		log.Printf("❌ Image processing failed: %v", err)
		return ErrorResponse(c, http.StatusBadRequest, "Image processing failed", "PROCESSING_FAILED", err.Error())
	}

	log.Printf("✅ Image compressed: %d bytes → %d bytes", file.Size, len(compressedData))

	// Create user-specific directory
	userDir := helper.GetUserUploadDir(userClaims.UserID)
	if err := os.MkdirAll(userDir, 0750); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to create user directory", "DIRECTORY_ERROR", err.Error())
	}

	// Get file path (always overwrites existing avatar)
	filePath := helper.GetUserAvatarPath(userClaims.UserID)
	avatarURL := helper.GetUserAvatarURL(userClaims.UserID)

	// Delete old avatar if exists (will be overwritten anyway, but good practice)
	if _, err := os.Stat(filePath); err == nil {
		log.Printf("🗑️ Overwriting existing avatar: %s", filePath)
	}

	// Save compressed file
	if err := saveCompressedFile(filePath, compressedData); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to save file", "SAVE_ERROR", err.Error())
	}

	// Get user for database update
	user, err := model.GetUserByID(userClaims.UserID)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to get user data", "DATABASE_ERROR", err.Error())
	}

	// Update user avatar_url in database
	user.AvatarURL = sql.NullString{String: avatarURL, Valid: true}
	err = model.UpdateUser(user)
	if err != nil {
		// Rollback: delete uploaded file
		helper.DeleteFile(filePath)

		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update user profile", "DATABASE_ERROR", err.Error())
	}

	// Log upload to audit_logs
	_ = model.LogAction(&model.AuditLog{
		UserID:       sql.NullInt64{Int64: userClaims.UserID, Valid: true},
		Action:       "avatar.upload",
		ResourceType: sql.NullString{String: "user", Valid: true},
		ResourceID:   sql.NullString{String: userClaims.Username, Valid: true},
		Details: map[string]interface{}{
			"original_filename": file.Filename,
			"original_size":     file.Size,
			"compressed_size":   len(compressedData),
			"format":            "webp",
			"saved_as":          filepath.Base(filePath),
		},
		IPAddress: sql.NullString{String: c.RealIP(), Valid: true},
		UserAgent: sql.NullString{String: c.Request().UserAgent(), Valid: true},
	})

	log.Printf("✅ Avatar uploaded successfully for user %d: %s", userClaims.UserID, avatarURL)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Avatar uploaded successfully",
		"data": map[string]interface{}{
			"avatar_url":      avatarURL,
			"original_size":   file.Size,
			"compressed_size": len(compressedData),
			"format":          "webp",
		},
	})
}

// saveCompressedFile saves compressed byte data to file
func saveCompressedFile(filePath string, data []byte) error {
	// filePath is built by helper.GetUserAvatarPath from the caller's own user id
	// and a fixed file name. No request value reaches the path.
	// #nosec G304
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Set file permissions (owner read-write only)
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	// Write data
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

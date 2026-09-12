// internal/handler/file_upload.go
package handler

import (
	"database/sql"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// readAvatarUpload validates the uploaded avatar and returns its compressed
// WebP bytes. The returned error is an already-written ErrorResponse.
func readAvatarUpload(c echo.Context, file *multipart.FileHeader, userID int64) ([]byte, error) {
	if err := helper.ValidateImageFile(file); err != nil {
		return nil, ErrorResponse(c, http.StatusBadRequest, "File is not a valid image", "INVALID_FILE", err.Error())
	}

	src, err := file.Open()
	if err != nil {
		return nil, ErrorResponse(c, http.StatusInternalServerError, "Failed to open uploaded file", "FILE_OPEN_ERROR", err.Error())
	}
	defer func() { _ = src.Close() }()

	// Check magic bytes (file signature validation)
	if err := helper.CheckMagicBytes(src); err != nil {
		return nil, ErrorResponse(c, http.StatusBadRequest, "File is not a valid image", "INVALID_FILE_SIGNATURE", err.Error())
	}

	log.Printf("📸 Processing avatar upload for user %d (original size: %d bytes)", userID, file.Size)

	compressedData, err := helper.CompressAndResize(src, file)
	if err != nil {
		log.Printf("❌ Image processing failed: %v", err)
		return nil, ErrorResponse(c, http.StatusBadRequest, "Image processing failed", "PROCESSING_FAILED", err.Error())
	}

	log.Printf("✅ Image compressed: %d bytes → %d bytes", file.Size, len(compressedData))
	return compressedData, nil
}

// storeAvatar writes the avatar to the user's upload directory, overwriting any
// previous one, and returns its path. The returned error is an already-written
// ErrorResponse.
func storeAvatar(c echo.Context, userID int64, data []byte) (string, error) {
	if err := os.MkdirAll(helper.GetUserUploadDir(userID), 0750); err != nil {
		return "", ErrorResponse(c, http.StatusInternalServerError, "Failed to create user directory", "DIRECTORY_ERROR", err.Error())
	}

	filePath := helper.GetUserAvatarPath(userID)
	if err := saveCompressedFile(filePath, data); err != nil {
		return "", ErrorResponse(c, http.StatusInternalServerError, "Failed to save file", "SAVE_ERROR", err.Error())
	}
	return filePath, nil
}

// persistAvatarURL points the user record at the new avatar. A database failure
// removes the just-written file so no orphan is left behind.
func persistAvatarURL(c echo.Context, userID int64, avatarURL, filePath string) error {
	user, err := model.GetUserByID(userID)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to get user data", "DATABASE_ERROR", err.Error())
	}

	user.AvatarURL = sql.NullString{String: avatarURL, Valid: true}
	if err := model.UpdateUser(user); err != nil {
		if delErr := helper.DeleteFile(filePath); delErr != nil {
			log.Printf("⚠️ Failed to remove orphaned avatar %s: %v", filepath.Base(filePath), delErr)
		}
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update user profile", "DATABASE_ERROR", err.Error())
	}
	return nil
}

// logAvatarUpload records the upload in audit_logs.
func logAvatarUpload(c echo.Context, userClaims *service.Claims, file *multipart.FileHeader, filePath string, compressedSize int) {
	err := model.LogAction(&model.AuditLog{
		UserID:       sql.NullInt64{Int64: userClaims.UserID, Valid: true},
		Action:       "avatar.upload",
		ResourceType: sql.NullString{String: "user", Valid: true},
		ResourceID:   sql.NullString{String: userClaims.Username, Valid: true},
		Details: map[string]any{
			"original_filename": file.Filename,
			"original_size":     file.Size,
			"compressed_size":   compressedSize,
			"format":            "webp",
			"saved_as":          filepath.Base(filePath),
		},
		IPAddress: sql.NullString{String: c.RealIP(), Valid: true},
		UserAgent: sql.NullString{String: c.Request().UserAgent(), Valid: true},
	})
	if err != nil {
		log.Printf("⚠️ Failed to write avatar upload audit log for user %d: %v", userClaims.UserID, err)
	}
}

// UploadAvatar handles avatar upload
// POST /api/me/avatar
func UploadAvatar(c echo.Context) error {
	userClaims, ok := c.Get("user_claims").(*service.Claims)
	if !ok {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	file, err := c.FormFile("avatar")
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "No file uploaded", "NO_FILE", err.Error())
	}

	compressedData, errResp := readAvatarUpload(c, file, userClaims.UserID)
	if errResp != nil {
		return errResp
	}

	filePath, errResp := storeAvatar(c, userClaims.UserID, compressedData)
	if errResp != nil {
		return errResp
	}

	avatarURL := helper.GetUserAvatarURL(userClaims.UserID)
	if errResp := persistAvatarURL(c, userClaims.UserID, avatarURL, filePath); errResp != nil {
		return errResp
	}

	logAvatarUpload(c, userClaims, file, filePath, len(compressedData))
	log.Printf("✅ Avatar uploaded successfully for user %d: %s", userClaims.UserID, avatarURL)

	return SuccessResponse(c, http.StatusOK, "Avatar uploaded successfully", map[string]any{
		"avatar_url":      avatarURL,
		"original_size":   file.Size,
		"compressed_size": len(compressedData),
		"format":          "webp",
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

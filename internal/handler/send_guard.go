// internal/handler/send_guard.go
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
	"go.mau.fi/whatsmeow/types"
)

// resolveSenderInstance maps a sender phone number to its active instance and
// verifies the caller may use it. The second result is a written ErrorResponse
// for the caller to propagate.
func resolveSenderInstance(c echo.Context, phoneNumber string) (*model.Instance, error) {
	inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveInstance) {
			return nil, ErrorResponse(c, 404, "No active instance for this phone number", "NO_ACTIVE_INSTANCE", "Please login / scan QR for this number")
		}
		return nil, ErrorResponse(c, 500, "Failed to get instance for this phone number", "DB_ERROR", err.Error())
	}

	userClaims, _ := c.Get("user_claims").(*service.Claims)
	if userClaims != nil && userClaims.Role != "admin" {
		if _, err := model.CheckUserInstancePermission(userClaims.UserID, inst.InstanceID); err != nil {
			return nil, ErrorResponse(c, 403, "Insufficient permission to use this phone number", "FORBIDDEN", "")
		}
	}

	return inst, nil
}

// resolveRecipient normalizes a destination number and confirms it is on
// WhatsApp. The check is skipped when SKIP_WHATSAPP_REGISTRATION_CHECK is set.
// The second result is a written ErrorResponse for the caller to propagate.
func resolveRecipient(c echo.Context, session *model.Session, to string) (types.JID, error) {
	recipient, err := helper.FormatPhoneNumber(to)
	if err != nil {
		return types.JID{}, ErrorResponse(c, 400, "Invalid phone number", "INVALID_PHONE", err.Error())
	}

	if helper.SkipWhatsAppRegistrationCheck() {
		return recipient, nil
	}

	isRegistered, err := session.Client.IsOnWhatsApp(context.Background(), []string{recipient.User})
	if err != nil {
		return types.JID{}, ErrorResponse(c, 500, "Failed to verify phone number", "VERIFICATION_FAILED", err.Error())
	}
	if len(isRegistered) == 0 || !isRegistered[0].IsIn {
		return types.JID{}, ErrorResponse(c, 400, "Phone number is not registered on WhatsApp", "PHONE_NOT_REGISTERED",
			"Please check the number or ask recipient to install WhatsApp")
	}

	return recipient, nil
}

// toWhatsmeowMediaType maps the project's media type names onto whatsmeow's.
// Anything unrecognised is uploaded as a document.
func toWhatsmeowMediaType(mediaType string) whatsmeow.MediaType {
	switch mediaType {
	case "image":
		return whatsmeow.MediaImage
	case "video":
		return whatsmeow.MediaVideo
	case "audio":
		return whatsmeow.MediaAudio
	default:
		return whatsmeow.MediaDocument
	}
}

// readUploadedMedia reads the "file" form field under the size cap for its
// media type, then re-detects the type from the content's magic bytes so a
// spoofed extension cannot bypass the cap. The last result is a written
// ErrorResponse for the caller to propagate.
func readUploadedMedia(c echo.Context) (fileData []byte, mediaType string, filename string, errResp error) {
	file, err := c.FormFile("file")
	if err != nil {
		return nil, "", "", ErrorResponse(c, 400, "File is required", "FILE_REQUIRED", err.Error())
	}

	// Type from the filename; re-verified against the content below.
	mediaType = helper.DetectMediaType(file.Filename)

	maxSize := getMaxFileSize(mediaType)
	fileData, err = readMultipartUpload(file, maxSize)
	if err != nil {
		return nil, "", "", ErrorResponse(c, 400, "File too large or unreadable", "FILE_TOO_LARGE",
			fmt.Sprintf("Type: %s, Max: %d bytes, Error: %v", mediaType, maxSize, err))
	}

	sniffedType := helper.DetectMediaTypeFromBytes(fileData)
	if sniffedType != mediaType {
		sniffedMax := getMaxFileSize(sniffedType)
		if len(fileData) > sniffedMax {
			return nil, "", "", ErrorResponse(c, 400, "File too large for detected media type", "FILE_TOO_LARGE",
				fmt.Sprintf("Detected: %s, Max: %d bytes, Got: %d", sniffedType, sniffedMax, len(fileData)))
		}
		mediaType = sniffedType
	}

	return fileData, mediaType, file.Filename, nil
}

// downloadRemoteMedia fetches a media URL and resolves its type, preferring the
// caller's declared type over the one detected from the filename. The last
// result is a written ErrorResponse for the caller to propagate.
func downloadRemoteMedia(c echo.Context, mediaURL, declaredType string) (fileData []byte, mediaType string, filename string, errResp error) {
	fileData, filename, err := helper.DownloadFile(mediaURL)
	if err != nil {
		return nil, "", "", ErrorResponse(c, 500, "Failed to download file", "DOWNLOAD_FAILED", err.Error())
	}

	mediaType = declaredType
	if mediaType == "" {
		mediaType = helper.DetectMediaType(filename)
	}

	maxSize := getMaxFileSize(mediaType)
	if len(fileData) > maxSize {
		return nil, "", "", ErrorResponse(c, 400, "File too large", "FILE_TOO_LARGE",
			fmt.Sprintf("File size: %d bytes, Max allowed: %d bytes (%s)", len(fileData), maxSize, mediaType))
	}

	return fileData, mediaType, filename, nil
}

// uploadAndSendMedia uploads the payload to WhatsApp and sends it to recipient
// with the project's typing delay applied. The last result is a written
// ErrorResponse for the caller to propagate.
func uploadAndSendMedia(c echo.Context, session *model.Session, recipient types.JID,
	fileData []byte, mediaType, filename, caption string) (*whatsmeow.SendResponse, error) {

	uploaded, err := session.Client.Upload(context.Background(), fileData, toWhatsmeowMediaType(mediaType))
	if err != nil {
		return nil, ErrorResponse(c, 500, "Failed to upload media", "UPLOAD_FAILED",
			fmt.Sprintf("File: %s, Size: %d bytes, Type: %s, Error: %v", filename, len(fileData), mediaType, err))
	}

	helper.ApplyTypingDelay(session.Client, recipient, len(caption))

	msg := helper.CreateMediaMessage(uploaded, caption, filename, mediaType)
	resp, err := session.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		return nil, ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	return &resp, nil
}

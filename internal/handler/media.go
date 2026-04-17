package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
	"go.mau.fi/whatsmeow"
)

// Request body for sending media from URL
type SendMediaRequest struct {
	To        string `json:"to" validate:"required"`
	MediaURL  string `json:"mediaUrl" validate:"required"`
	Caption   string `json:"caption"`
	MediaType string `json:"mediaType"` // image, video, document, audio
}

// BY INSTANCE ID
// POST /send/:instanceId/media (upload file)
func SendMediaFile(c echo.Context) error {
	instanceID := c.Param("instanceId")

	to := c.FormValue("to")
	caption := c.FormValue("caption")

	if to == "" {
		return ErrorResponse(c, 400, "Field 'to' is required", "VALIDATION_ERROR", "")
	}

	// 1. CHECK SESSION EXISTS
	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "Please login first")
	}

	// 2. CHECK CONNECTION FLAG (from memory/database)
	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "Please check /status endpoint")
	}

	// 3. CHECK REAL WHATSAPP CONNECTION (websocket)
	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "Please reconnect")
	}

	// 4. CHECK IF LOGGED IN (has JID)
	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "Please scan QR code first")
	}

	// 5. FORMAT & VALIDATE PHONE NUMBER
	recipient, err := helper.FormatPhoneNumber(to)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid phone number", "INVALID_PHONE", err.Error())
	}

	if !helper.ShouldSkipValidation(to) {
		isRegistered, err := session.Client.IsOnWhatsApp(context.Background(), []string{recipient.User})
		if err != nil {
			return ErrorResponse(c, 500, "Failed to verify phone number", "VERIFICATION_FAILED", err.Error())
		}

		if len(isRegistered) == 0 || !isRegistered[0].IsIn {
			return ErrorResponse(c, 400, "Phone number is not registered on WhatsApp", "PHONE_NOT_REGISTERED",
				"Please check the number or ask recipient to install WhatsApp")
		}
	}

	// 7. GET & VALIDATE FILE
	file, err := c.FormFile("file")
	if err != nil {
		return ErrorResponse(c, 400, "File is required", "FILE_REQUIRED", err.Error())
	}

	// 8. DETECT MEDIA TYPE (from filename; re-verified against content below)
	mediaType := helper.DetectMediaType(file.Filename)

	// 9. READ FILE WITH SIZE LIMIT (pre-check + streaming cap)
	maxSize := getMaxFileSize(mediaType)
	fileData, err := readMultipartUpload(file, maxSize)
	if err != nil {
		return ErrorResponse(c, 400, "File too large or unreadable", "FILE_TOO_LARGE",
			fmt.Sprintf("Type: %s, Max: %d bytes, Error: %v", mediaType, maxSize, err))
	}

	// 9b. VERIFY CONTENT TYPE by magic-byte sniffing — reject spoofed extensions.
	sniffedType := helper.DetectMediaTypeFromBytes(fileData)
	if sniffedType != mediaType {
		sniffedMax := getMaxFileSize(sniffedType)
		if len(fileData) > sniffedMax {
			return ErrorResponse(c, 400, "File too large for detected media type", "FILE_TOO_LARGE",
				fmt.Sprintf("Detected: %s, Max: %d bytes, Got: %d", sniffedType, sniffedMax, len(fileData)))
		}
		mediaType = sniffedType
	}

	// 10. CONVERT MEDIA TYPE
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

	// 11. UPLOAD TO WHATSAPP
	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media", "UPLOAD_FAILED",
			fmt.Sprintf("Type: %s, Size: %d bytes, Error: %v", mediaType, len(fileData), err))
	}

	// Typing delay simulation
	messageLength := len(caption)
	helper.ApplyTypingDelay(session.Client, recipient, messageLength)

	// 12. CREATE MESSAGE
	msg := helper.CreateMediaMessage(uploaded, caption, file.Filename, mediaType)

	// 13. SEND MESSAGE
	resp, err := session.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	// 14. SUCCESS RESPONSE
	return SuccessResponse(c, 200, "Media sent successfully", map[string]interface{}{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"to":        to,
		"mediaType": mediaType,
		"fileName":  file.Filename,
		"fileSize":  len(fileData),
		"verified":  true,
	})
}

// BY INSTANCE ID
// POST /send/:instanceId/media-url (from URL)
func SendMediaURL(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req SendMediaRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.To == "" || req.MediaURL == "" {
		return ErrorResponse(c, 400, "Fields 'to' and 'mediaUrl' are required", "VALIDATION_ERROR", "")
	}

	// 1. CHECK SESSION EXISTS
	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "Please login first")
	}

	// 2. CHECK CONNECTION FLAG (from memory/database)
	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "Please check /status endpoint")
	}

	// 3. CHECK REAL WHATSAPP CONNECTION (websocket)
	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "Please reconnect")
	}

	// 4. CHECK IF LOGGED IN (has JID)
	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "Please scan QR code first")
	}

	// 5. FORMAT & VALIDATE PHONE NUMBER
	recipient, err := helper.FormatPhoneNumber(req.To)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid phone number", "INVALID_PHONE", err.Error())
	}

	if !helper.ShouldSkipValidation(req.To) {
		isRegistered, err := session.Client.IsOnWhatsApp(context.Background(), []string{recipient.User})
		if err != nil {
			return ErrorResponse(c, 500, "Failed to verify phone number", "VERIFICATION_FAILED", err.Error())
		}

		if len(isRegistered) == 0 || !isRegistered[0].IsIn {
			return ErrorResponse(c, 400, "Phone number is not registered on WhatsApp", "PHONE_NOT_REGISTERED",
				"Please check the number or ask recipient to install WhatsApp")
		}
	}

	// 7. DOWNLOAD FILE FROM URL
	fmt.Printf("Downloading from: %s\n", req.MediaURL)
	fileData, filename, err := helper.DownloadFile(req.MediaURL)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to download file", "DOWNLOAD_FAILED", err.Error())
	}
	fmt.Printf("Downloaded: %s (%d bytes)\n", filename, len(fileData))

	// 8. DETECT MEDIA TYPE
	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = helper.DetectMediaType(filename)
	}
	fmt.Printf("Detected media type: %s\n", mediaType)

	// 9. VALIDATE FILE SIZE
	maxSize := getMaxFileSize(mediaType)
	if len(fileData) > maxSize {
		return ErrorResponse(c, 400, "File too large", "FILE_TOO_LARGE",
			fmt.Sprintf("File size: %d bytes, Max allowed: %d bytes (%s)", len(fileData), maxSize, mediaType))
	}

	// 10. CONVERT MEDIA TYPE
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

	// 11. UPLOAD TO WHATSAPP
	fmt.Printf("Uploading to WhatsApp as: %s\n", whatsmeowMediaType)
	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media to WhatsApp", "UPLOAD_FAILED",
			fmt.Sprintf("File: %s, Size: %d bytes, Type: %s, Error: %v", filename, len(fileData), mediaType, err))
	}

	// Typing delay simulation
	messageLength := len(req.Caption)
	helper.ApplyTypingDelay(session.Client, recipient, messageLength)

	// 12. CREATE MESSAGE
	msg := helper.CreateMediaMessage(uploaded, req.Caption, filename, mediaType)

	// 13. SEND MESSAGE
	resp, err := session.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	// 14. SUCCESS RESPONSE
	return SuccessResponse(c, 200, "Media sent successfully", map[string]interface{}{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"to":        req.To,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
		"verified":  true,
	})
}

// BY PHONE NUMBER
// POST /send/by-number/:phoneNumber/media-url
func SendMediaURLByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	var req SendMediaRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.To == "" || req.MediaURL == "" {
		return ErrorResponse(c, 400, "Fields 'to' and 'mediaUrl' are required", "VALIDATION_ERROR", "")
	}

	inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveInstance) {
			return ErrorResponse(c, 404, "No active instance for this phone number", "NO_ACTIVE_INSTANCE", "Please login / scan QR for this number")
		}
		return ErrorResponse(c, 500, "Failed to get instance for this phone number", "DB_ERROR", err.Error())
	}

	// 1.5) Check Permission
	userClaims, _ := c.Get("user_claims").(*service.Claims)
	if userClaims != nil && userClaims.Role != "admin" {
		_, err := model.CheckUserInstancePermission(userClaims.UserID, inst.InstanceID)
		if err != nil {
			return ErrorResponse(c, 403, "Insufficient permission to use this phone number", "FORBIDDEN", "")
		}
	}

	session, err := service.GetSession(inst.InstanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "Please login / reconnect first")
	}

	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "Please check /status endpoint")
	}

	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "Please reconnect")
	}

	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "Please scan QR code first")
	}

	recipient, err := helper.FormatPhoneNumber(req.To)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid phone number", "INVALID_PHONE", err.Error())
	}

	if !helper.ShouldSkipValidation(req.To) {
		isRegistered, err := session.Client.IsOnWhatsApp(context.Background(), []string{recipient.User})
		if err != nil {
			return ErrorResponse(c, 500, "Failed to verify phone number", "VERIFICATION_FAILED", err.Error())
		}

		if len(isRegistered) == 0 || !isRegistered[0].IsIn {
			return ErrorResponse(c, 400, "Phone number is not registered on WhatsApp", "PHONE_NOT_REGISTERED", "Please check the number or ask recipient to install WhatsApp")
		}
	}

	fmt.Printf("Downloading from: %s\n", req.MediaURL)
	fileData, filename, err := helper.DownloadFile(req.MediaURL)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to download file", "DOWNLOAD_FAILED", err.Error())
	}
	fmt.Printf("Downloaded: %s (%d bytes)\n", filename, len(fileData))

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = helper.DetectMediaType(filename)
	}
	fmt.Printf("Detected media type: %s\n", mediaType)

	maxSize := getMaxFileSize(mediaType)
	if len(fileData) > maxSize {
		return ErrorResponse(c, 400, "File too large", "FILE_TOO_LARGE", fmt.Sprintf("File size: %d bytes, Max allowed: %d bytes (%s)", len(fileData), maxSize, mediaType))
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

	fmt.Printf("Uploading to WhatsApp as: %s\n", whatsmeowMediaType)
	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media to WhatsApp", "UPLOAD_FAILED", fmt.Sprintf("File: %s, Size: %d bytes, Type: %s, Error: %v", filename, len(fileData), mediaType, err))
	}

	// Typing delay simulation
	messageLength := len(req.Caption)
	helper.ApplyTypingDelay(session.Client, recipient, messageLength)

	msg := helper.CreateMediaMessage(uploaded, req.Caption, filename, mediaType)

	resp, err := session.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	return SuccessResponse(c, 200, "Media sent successfully", map[string]interface{}{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"to":        req.To,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
		"verified":  true,
	})
}

// POST /send/by-number/:phoneNumber/media-file
func SendMediaFileByNumber(c echo.Context) error {
	// 0. Sender WhatsApp number from path (format: 905545...)
	phoneNumber := c.Param("phoneNumber")

	to := c.FormValue("to")
	caption := c.FormValue("caption")

	if to == "" {
		return ErrorResponse(c, 400, "Field 'to' is required", "VALIDATION_ERROR", "")
	}

	// 1. Find active instance by sender number
	inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveInstance) {
			return ErrorResponse(c, 404, "No active instance for this phone number", "NO_ACTIVE_INSTANCE", "Please login / scan QR for this number")
		}
		return ErrorResponse(c, 500, "Failed to get instance for this phone number", "DB_ERROR", err.Error())
	}

	// 1.5) Check Permission
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
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "Please login / reconnect first")
	}

	// 3. CHECK CONNECTION FLAG (from memory/database)
	if !session.IsConnected {
		return ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "Please check /status endpoint")
	}

	// 4. CHECK REAL WHATSAPP CONNECTION (websocket)
	if !session.Client.IsConnected() {
		return ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "Please reconnect")
	}

	// 5. CHECK IF LOGGED IN (has JID)
	if session.Client.Store.ID == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "Please scan QR code first")
	}

	// 6. FORMAT & VALIDATE DESTINATION PHONE NUMBER
	recipient, err := helper.FormatPhoneNumber(to)
	if err != nil {
		return ErrorResponse(c, 400, "Invalid phone number", "INVALID_PHONE", err.Error())
	}

	if !helper.ShouldSkipValidation(to) {
		isRegistered, err := session.Client.IsOnWhatsApp(context.Background(), []string{recipient.User})
		if err != nil {
			return ErrorResponse(c, 500, "Failed to verify phone number", "VERIFICATION_FAILED", err.Error())
		}

		if len(isRegistered) == 0 || !isRegistered[0].IsIn {
			return ErrorResponse(c, 400, "Phone number is not registered on WhatsApp", "PHONE_NOT_REGISTERED", "Please check the number or ask recipient to install WhatsApp")
		}
	}

	// 8. GET & VALIDATE FILE
	file, err := c.FormFile("file")
	if err != nil {
		return ErrorResponse(c, 400, "File is required", "FILE_REQUIRED", err.Error())
	}

	// 9. DETECT MEDIA TYPE (from filename; re-verified against content below)
	mediaType := helper.DetectMediaType(file.Filename)

	// 10. READ FILE WITH SIZE LIMIT (pre-check + streaming cap)
	maxSize := getMaxFileSize(mediaType)
	fileData, err := readMultipartUpload(file, maxSize)
	if err != nil {
		return ErrorResponse(c, 400, "File too large or unreadable", "FILE_TOO_LARGE",
			fmt.Sprintf("Type: %s, Max: %d bytes, Error: %v", mediaType, maxSize, err))
	}

	// 10b. VERIFY CONTENT TYPE by magic-byte sniffing — reject spoofed extensions.
	sniffedType := helper.DetectMediaTypeFromBytes(fileData)
	if sniffedType != mediaType {
		sniffedMax := getMaxFileSize(sniffedType)
		if len(fileData) > sniffedMax {
			return ErrorResponse(c, 400, "File too large for detected media type", "FILE_TOO_LARGE",
				fmt.Sprintf("Detected: %s, Max: %d bytes, Got: %d", sniffedType, sniffedMax, len(fileData)))
		}
		mediaType = sniffedType
	}

	// 11. CONVERT MEDIA TYPE
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

	// 12. UPLOAD TO WHATSAPP
	uploaded, err := session.Client.Upload(context.Background(), fileData, whatsmeowMediaType)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to upload media", "UPLOAD_FAILED", fmt.Sprintf("Type: %s, Size: %d bytes, Error: %v", mediaType, len(fileData), err))
	}

	// Typing delay simulation
	messageLength := len(caption)
	helper.ApplyTypingDelay(session.Client, recipient, messageLength)

	// 13. CREATE MESSAGE
	msg := helper.CreateMediaMessage(uploaded, caption, file.Filename, mediaType)

	// 14. SEND MESSAGE
	resp, err := session.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		return ErrorResponse(c, 500, "Failed to send media", "SEND_FAILED", err.Error())
	}

	// 15. SUCCESS RESPONSE
	return SuccessResponse(c, 200, "Media sent successfully", map[string]interface{}{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"to":        to,
		"mediaType": mediaType,
		"fileName":  file.Filename,
		"fileSize":  len(fileData),
		"verified":  true,
	})
}

// readMultipartUpload streams a multipart file into memory while enforcing a
// hard cap. It returns an error if the advertised size already exceeds the
// limit, or if the stream ultimately exceeds it despite the header.
func readMultipartUpload(file *multipart.FileHeader, maxSize int) ([]byte, error) {
	if maxSize > 0 && file.Size > int64(maxSize) {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", file.Size, maxSize)
	}

	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	limit := int64(maxSize) + 1
	buf := &bytes.Buffer{}
	n, err := io.Copy(buf, io.LimitReader(src, limit))
	if err != nil {
		return nil, err
	}
	if n > int64(maxSize) {
		return nil, fmt.Errorf("file too large: exceeds %d bytes", maxSize)
	}
	return buf.Bytes(), nil
}

// Helper: Get max file size per media type (WhatsApp limits)
func getMaxFileSize(mediaType string) int {
	switch mediaType {
	case "image":
		return helper.GetEnvAsInt("MAX_FILE_SIZE_IMAGE_MB", 5) * 1024 * 1024
	case "video":
		return helper.GetEnvAsInt("MAX_FILE_SIZE_VIDEO_MB", 16) * 1024 * 1024
	case "audio":
		return helper.GetEnvAsInt("MAX_FILE_SIZE_AUDIO_MB", 16) * 1024 * 1024
	default: // document
		return helper.GetEnvAsInt("MAX_FILE_SIZE_DOCUMENT_MB", 100) * 1024 * 1024
	}
}

package handler

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"

	"charon/internal/helper"

	"github.com/labstack/echo/v4"
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

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	recipient, errResp := resolveRecipient(c, session, to)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := readUploadedMedia(c)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, recipient, fileData, mediaType, filename, caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent successfully", map[string]any{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"to":        to,
		"mediaType": mediaType,
		"fileName":  filename,
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

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	recipient, errResp := resolveRecipient(c, session, req.To)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := downloadRemoteMedia(c, req.MediaURL, req.MediaType)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, recipient, fileData, mediaType, filename, req.Caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent successfully", map[string]any{
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

	inst, errResp := resolveSenderInstance(c, phoneNumber)
	if errResp != nil {
		return errResp
	}

	session, errResp := requireConnectedSession(c, inst.InstanceID)
	if errResp != nil {
		return errResp
	}

	recipient, errResp := resolveRecipient(c, session, req.To)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := downloadRemoteMedia(c, req.MediaURL, req.MediaType)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, recipient, fileData, mediaType, filename, req.Caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent successfully", map[string]any{
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

	inst, errResp := resolveSenderInstance(c, phoneNumber)
	if errResp != nil {
		return errResp
	}

	session, errResp := requireConnectedSession(c, inst.InstanceID)
	if errResp != nil {
		return errResp
	}

	recipient, errResp := resolveRecipient(c, session, to)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := readUploadedMedia(c)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, recipient, fileData, mediaType, filename, caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent successfully", map[string]any{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"to":        to,
		"mediaType": mediaType,
		"fileName":  filename,
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
	defer func() { _ = src.Close() }()

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

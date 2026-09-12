package handler

import (
	"context"

	"charon/internal/helper"
	"charon/internal/model"

	"github.com/labstack/echo/v4"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// Request body for sending a message
type SendMessageRequest struct {
	To      string `json:"to" validate:"required"`
	Message string `json:"message" validate:"required"`
}

// POST /send/:instanceId
func SendMessage(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req SendMessageRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.To == "" || req.Message == "" {
		return ErrorResponse(c, 400, "Field 'to' and 'message' are required", "VALIDATION_ERROR", "")
	}

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	recipient, errResp := resolveRecipient(c, session, req.To)
	if errResp != nil {
		return errResp
	}

	resp, errResp := sendTextMessage(c, session, recipient, req.Message)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Message sent successfully", map[string]any{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"to":        req.To,
		"verified":  true,
	})
}

// sendTextMessage applies the typing delay and sends a plain text message. The
// second result is a written ErrorResponse for the caller to propagate.
func sendTextMessage(c echo.Context, session *model.Session, recipient types.JID, text string) (*whatsmeow.SendResponse, error) {
	helper.ApplyTypingDelay(session.Client, recipient, len(text))

	msg := &waE2E.Message{Conversation: &text}
	resp, err := session.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		return nil, ErrorResponse(c, 500, "Failed to send message", "SEND_FAILED", err.Error())
	}
	return &resp, nil
}

// POST /send/by-number/:phoneNumber
func SendMessageByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	var req SendMessageRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.To == "" || req.Message == "" {
		return ErrorResponse(c, 400, "Field 'to' and 'message' are required", "VALIDATION_ERROR", "")
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

	resp, errResp := sendTextMessage(c, session, recipient, req.Message)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Message sent successfully", map[string]any{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"from":      inst.PhoneNumber.String, // canonical sender number
		"to":        req.To,
		"verified":  true,
	})
}

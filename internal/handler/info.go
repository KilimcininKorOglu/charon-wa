package handler

import (
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// GET /info-device/:instanceId
func GetDeviceInfo(c echo.Context) error {
	instanceID := c.Param("instanceId")

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

	// 5. GET JID & PHONE NUMBER
	deviceJIDPtr := session.Client.Store.ID // *types.JID
	if deviceJIDPtr == nil {
		return ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "Please scan QR code first")
	}

	deviceJID := *deviceJIDPtr // types.JID (deref)
	phoneNumber := deviceJID.User
	fullJID := deviceJID.String()

	// 7. SUCCESS RESPONSE
	return SuccessResponse(c, 200, "Device info retrieved", map[string]any{
		"instanceId":  instanceID,
		"jid":         fullJID,
		"phoneNumber": phoneNumber,
	})
}

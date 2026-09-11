package handler

import (
	"github.com/labstack/echo/v4"
)

// GET /info-device/:instanceId
func GetDeviceInfo(c echo.Context) error {
	instanceID := c.Param("instanceId")

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
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

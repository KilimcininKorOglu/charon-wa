// internal/handler/session_guard.go
package handler

import (
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// requireConnectedSession resolves an instance's WhatsApp session and verifies
// it is paired and connected before a handler uses it.
//
// The second result is a fully written ErrorResponse, so a caller propagates it
// unchanged:
//
//	session, errResp := requireConnectedSession(c, instanceID)
//	if errResp != nil {
//		return errResp
//	}
func requireConnectedSession(c echo.Context, instanceID string) (*model.Session, error) {
	session, err := service.GetSession(instanceID)
	if err != nil {
		return nil, ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "Please login first")
	}

	if !session.IsConnected {
		return nil, ErrorResponse(c, 400, "Session is not connected", "NOT_CONNECTED", "Please check /status endpoint")
	}

	if !session.Client.IsConnected() {
		return nil, ErrorResponse(c, 400, "WhatsApp connection lost", "CONNECTION_LOST", "Please reconnect")
	}

	if session.Client.Store.ID == nil {
		return nil, ErrorResponse(c, 400, "Not logged in", "NOT_LOGGED_IN", "Please scan QR code first")
	}

	return session, nil
}

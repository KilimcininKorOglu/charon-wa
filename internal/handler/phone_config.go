package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"charon/config"
)

// GetPhoneConfigHandler reports the phone number defaults the backend parses
// with, so the frontend validates a number exactly as the server will.
//
// It reads the same config global the parser reads. The system identity blob is
// deliberately not used for this: that one is admin-editable branding stored in
// the database, and it would drift from the value the parser actually applies.
//
// GET /api/system/phone-config
func GetPhoneConfigHandler(c echo.Context) error {
	return SuccessResponse(c, http.StatusOK, "Phone configuration retrieved", map[string]any{
		"defaultRegion": config.PhoneDefaultRegion,
	})
}

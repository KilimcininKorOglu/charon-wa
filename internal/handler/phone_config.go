package handler

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"charon/config"
	"charon/internal/model"
)

// UpdatePhoneConfigRequest carries the deployment-wide default region. An empty
// string is allowed and means every number must carry a country code.
type UpdatePhoneConfigRequest struct {
	DefaultRegion string `json:"default_region"`
}

// GetPhoneConfigHandler reports the phone number defaults the backend parses
// with, so the frontend validates a number exactly as the server will.
//
// It reads the same config global the parser reads. The system identity blob is
// deliberately not used for this: that one is admin-editable branding, and it
// would drift from the value the parser actually applies.
//
// GET /api/system/phone-config
func GetPhoneConfigHandler(c echo.Context) error {
	return SuccessResponse(c, http.StatusOK, "Phone configuration retrieved", map[string]any{
		"defaultRegion": config.PhoneRegion(),
	})
}

// UpdatePhoneConfigHandler sets the deployment-wide default region. It writes
// the database row first and only then swaps the in-memory value, so a failed
// write leaves the parser on the value it was already using.
//
// The worker picks the new value up on its next configuration reload; it runs
// as a separate process and does not share this memory.
//
// POST /api/system/phone-config
func UpdatePhoneConfigHandler(c echo.Context) error {
	var req UpdatePhoneConfigRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", err.Error())
	}

	region := strings.ToUpper(strings.TrimSpace(req.DefaultRegion))
	if region != "" && !config.IsSupportedRegion(region) {
		return ErrorResponse(c, http.StatusBadRequest,
			"Unknown region code", "INVALID_REGION",
			"Expected an ISO 3166-1 alpha-2 code such as TR, got "+region)
	}

	if err := model.UpdatePhoneConfig(&model.PhoneConfig{DefaultRegion: region}); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to save phone configuration", "DB_ERROR", err.Error())
	}

	config.SetPhoneRegion(region)

	return SuccessResponse(c, http.StatusOK, "Phone configuration updated", map[string]any{
		"defaultRegion": config.PhoneRegion(),
	})
}

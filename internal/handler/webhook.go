// internal/handler/webhook.go
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

type WebhookConfigRequest struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

// bindWebhookConfigRequest reads and validates the request body. The returned
// error is an already-written ErrorResponse.
func bindWebhookConfigRequest(c echo.Context) (WebhookConfigRequest, error) {
	var req WebhookConfigRequest
	if err := c.Bind(&req); err != nil {
		return req, ErrorResponse(c, http.StatusBadRequest,
			"Invalid request body", "INVALID_REQUEST", err.Error())
	}

	if req.URL == "" {
		return req, ErrorResponse(c, http.StatusBadRequest,
			"Field 'url' is required", "VALIDATION_ERROR", "")
	}

	// Validate webhook URL (scheme + no private IPs)
	if err := helper.ValidateExternalURL(req.URL); err != nil {
		return req, ErrorResponse(c, http.StatusBadRequest,
			"Invalid webhook URL", "INVALID_URL", err.Error())
	}
	return req, nil
}

// resolveWebhookSecret keeps the supplied secret, reuses the stored one, or
// generates a new 32-byte value when neither exists.
func resolveWebhookSecret(requested, existing string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	if existing != "" {
		return existing, nil
	}

	// generate new random secret (32 bytes -> 64 hex chars)
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// POST /api/instances/:instanceId/webhook
func SetWebhookConfig(c echo.Context) error {
	instanceID := c.Param("instanceId")

	if instanceID == "" {
		return ErrorResponse(c, http.StatusBadRequest,
			"instanceId is required", "VALIDATION_ERROR", "")
	}

	req, errResp := bindWebhookConfigRequest(c)
	if errResp != nil {
		return errResp
	}

	// get current instance (to know existing secret)
	inst, err := model.GetInstanceByInstanceID(instanceID)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound,
			"Instance not found", "INSTANCE_NOT_FOUND", "")
	}

	effectiveSecret, err := resolveWebhookSecret(req.Secret, inst.WebhookSecret.String)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError,
			"Failed to generate webhook secret", "WEBHOOK_SECRET_GENERATION_FAILED", err.Error())
	}

	// Update DB with url + effectiveSecret
	if err := model.UpdateInstanceWebhook(instanceID, req.URL, effectiveSecret); err != nil {
		if err.Error() == "instance_not_found" {
			return ErrorResponse(c, http.StatusNotFound,
				"Instance not found", "INSTANCE_NOT_FOUND", "")
		}

		return ErrorResponse(c, http.StatusInternalServerError,
			"Failed to update webhook config", "WEBHOOK_UPDATE_FAILED", err.Error())
	}

	// Invalidate cache after updating webhook config
	service.InvalidateWebhookCache(instanceID)

	return SuccessResponse(c, http.StatusOK, "Webhook config updated", map[string]any{
		"instanceId": instanceID,
		"webhookUrl": req.URL,
		"secret":     effectiveSecret, // user must store this securely
	})
}

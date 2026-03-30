package handler

import (
	"net/http"
	"strconv"

	"hermeswa/internal/model"

	"github.com/labstack/echo/v4"
)

type createAPIKeyRequest struct {
	Name        string `json:"name"`
	Application string `json:"application"`
}

// CreateAPIKey generates a new API key for the authenticated user
func CreateAPIKey(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	var req createAPIKeyRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	if req.Name == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Name is required", "VALIDATION_ERROR", "")
	}

	rawKey, key, err := model.CreateAPIKey(c.Request().Context(), int(claims.UserID), req.Name, req.Application)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to create API key", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusCreated, "API key created. Save this key — it won't be shown again.", map[string]interface{}{
		"id":          key.ID,
		"key":         rawKey,
		"key_prefix":  key.KeyPrefix,
		"name":        key.Name,
		"application": key.Application,
		"created_at":  key.CreatedAt,
	})
}

// ListAPIKeys returns all API keys for the authenticated user
func ListAPIKeys(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	keys, err := model.ListAPIKeys(c.Request().Context(), int(claims.UserID))
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to list API keys", "INTERNAL_ERROR", err.Error())
	}

	if keys == nil {
		keys = []model.APIKey{}
	}

	return SuccessResponse(c, http.StatusOK, "API keys retrieved", keys)
}

// DeleteAPIKey removes an API key
func DeleteAPIKey(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid key ID", "BAD_REQUEST", "")
	}

	if err := model.DeleteAPIKey(c.Request().Context(), id, int(claims.UserID)); err != nil {
		return ErrorResponse(c, http.StatusNotFound, "API key not found", "NOT_FOUND", "")
	}

	return SuccessResponse(c, http.StatusOK, "API key deleted", nil)
}

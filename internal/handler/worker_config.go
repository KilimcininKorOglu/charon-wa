package handler

import (
	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"
	"database/sql"
	"net/http"
	"slices"
	"strconv"

	"github.com/labstack/echo/v4"
)

type WorkerConfigRequest struct {
	WorkerName         string `json:"worker_name"`
	Circle             string `json:"circle"`
	Application        string `json:"application"`
	MessageType        string `json:"message_type"`
	IntervalMinSeconds int    `json:"interval_min_seconds"`
	IntervalSeconds    int    `json:"interval_seconds"` // Alias for backward compatibility
	IntervalMaxSeconds int    `json:"interval_max_seconds"`
	Enabled            *bool  `json:"enabled"`
	WebhookURL         string `json:"webhook_url"`
	WebhookSecret      string `json:"webhook_secret"`
	AllowMedia         *bool  `json:"allow_media"`
	UserID             int    `json:"user_id"` // Used for admin override
}

// getClaims is a helper to get user claims from context
func getClaims(c echo.Context) *service.Claims {
	claims, ok := c.Get("user_claims").(*service.Claims)
	if !ok {
		return nil
	}
	return claims
}

// GetWorkerConfigs retrieves blast outbox configurations based on user permissions
func GetWorkerConfigs(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	isAdmin := claims.Role == "admin"
	configs, err := model.GetWorkerConfigs(c.Request().Context(), int(claims.UserID), isAdmin)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve worker configs", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Worker configs retrieved successfully", configs)
}

// GetWorkerConfig retrieves a single worker configuration by ID
func GetWorkerConfig(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid config ID", "BAD_REQUEST", "")
	}

	config, err := model.GetWorkerConfigByID(c.Request().Context(), id)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve worker config", "INTERNAL_ERROR", err.Error())
	}

	if config == nil {
		return ErrorResponse(c, http.StatusNotFound, "Worker config not found", "NOT_FOUND", "")
	}

	// Authorization: user can only view own configs, admin can view all
	isAdmin := claims.Role == "admin"
	if !isAdmin && config.UserID != int(claims.UserID) {
		return ErrorResponse(c, http.StatusForbidden, "Access denied", "FORBIDDEN", "")
	}

	return SuccessResponse(c, http.StatusOK, "Worker config retrieved successfully", config)
}

// bindWorkerConfigRequest reads the request body, checks the required fields
// and the caller's circle access, and applies the request defaults in place.
// The second result is a written ErrorResponse for the caller to propagate.
func bindWorkerConfigRequest(c echo.Context, claims *service.Claims, isAdmin bool) (*WorkerConfigRequest, error) {
	var req WorkerConfigRequest
	if err := c.Bind(&req); err != nil {
		return nil, ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	if req.WorkerName == "" || req.Circle == "" || req.Application == "" {
		return nil, ErrorResponse(c, http.StatusBadRequest, "worker_name, circle, and application are required", "VALIDATION_ERROR", "")
	}

	if errResp := checkCircleAccess(c, claims, isAdmin, req.Circle); errResp != nil {
		return nil, errResp
	}

	if req.WebhookURL != "" {
		if err := helper.ValidateExternalURL(req.WebhookURL); err != nil {
			return nil, ErrorResponse(c, http.StatusBadRequest, "Invalid webhook URL", "INVALID_URL", err.Error())
		}
	}

	applyWorkerConfigDefaults(&req)
	return &req, nil
}

// checkCircleAccess confirms a non-admin caller owns an instance in the circle.
// The result is a written ErrorResponse, or nil when access is allowed.
func checkCircleAccess(c echo.Context, claims *service.Claims, isAdmin bool, circle string) error {
	if isAdmin {
		return nil
	}

	allowedCircles, err := model.GetUserInstanceCircles(claims.UserID)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to verify circle access", "INTERNAL_ERROR", err.Error())
	}
	if !slices.Contains(allowedCircles, circle) {
		return ErrorResponse(c, http.StatusForbidden, "You don't have access to instances in this circle", "FORBIDDEN", "")
	}
	return nil
}

// applyWorkerConfigDefaults fills in the message type and interval floor.
// interval_seconds is the legacy name for the minimum interval.
func applyWorkerConfigDefaults(req *WorkerConfigRequest) {
	if req.MessageType != "direct" && req.MessageType != "group" {
		req.MessageType = "direct" // Default
	}
	if req.IntervalMinSeconds < 1 && req.IntervalSeconds > 0 {
		req.IntervalMinSeconds = req.IntervalSeconds
	}
	if req.IntervalMinSeconds < 1 {
		req.IntervalMinSeconds = 10 // Default
	}
}

// applyWorkerConfigRequest copies a validated request onto a config, leaving
// the config's existing Enabled and AllowMedia values when the request omits them.
func applyWorkerConfigRequest(config *model.WorkerConfig, req *WorkerConfigRequest) {
	config.WorkerName = req.WorkerName
	config.Circle = req.Circle
	config.Application = req.Application
	config.MessageType = req.MessageType
	config.IntervalSeconds = req.IntervalMinSeconds
	config.IntervalMaxSeconds = req.IntervalMaxSeconds
	config.WebhookURL = sql.NullString{String: req.WebhookURL, Valid: req.WebhookURL != ""}
	config.WebhookSecret = sql.NullString{String: req.WebhookSecret, Valid: req.WebhookSecret != ""}

	if req.Enabled != nil {
		config.Enabled = *req.Enabled
	}
	if req.AllowMedia != nil {
		config.AllowMedia = *req.AllowMedia
	}
}

// loadOwnedWorkerConfig reads the config named by the :id path param and checks
// the caller may act on it. A user reaches only their own configs; an admin
// reaches all. The second result is a written ErrorResponse.
func loadOwnedWorkerConfig(c echo.Context, claims *service.Claims, isAdmin bool) (*model.WorkerConfig, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return nil, ErrorResponse(c, http.StatusBadRequest, "Invalid config ID", "BAD_REQUEST", "")
	}

	existingConfig, err := model.GetWorkerConfigByID(c.Request().Context(), id)
	if err != nil {
		return nil, ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve worker config", "INTERNAL_ERROR", err.Error())
	}
	if existingConfig == nil {
		return nil, ErrorResponse(c, http.StatusNotFound, "Worker config not found", "NOT_FOUND", "")
	}
	if !isAdmin && existingConfig.UserID != int(claims.UserID) {
		return nil, ErrorResponse(c, http.StatusForbidden, "Access denied", "FORBIDDEN", "")
	}

	return existingConfig, nil
}

// CreateWorkerConfig creates a new worker configuration
func CreateWorkerConfig(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}
	isAdmin := claims.Role == "admin"

	req, errResp := bindWorkerConfigRequest(c, claims, isAdmin)
	if errResp != nil {
		return errResp
	}

	config := model.WorkerConfig{
		Enabled:    true,  // Default
		AllowMedia: false, // Default to false
	}
	applyWorkerConfigRequest(&config, req)

	// Set user_id from authenticated user (admin can override)
	config.UserID = int(claims.UserID)
	if isAdmin && req.UserID != 0 {
		config.UserID = req.UserID
	}

	if err := model.CreateWorkerConfig(c.Request().Context(), &config); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to create worker config", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusCreated, "Worker config created successfully", config)
}

// UpdateWorkerConfig updates an existing worker configuration
func UpdateWorkerConfig(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}
	isAdmin := claims.Role == "admin"

	existingConfig, errResp := loadOwnedWorkerConfig(c, claims, isAdmin)
	if errResp != nil {
		return errResp
	}

	req, errResp := bindWorkerConfigRequest(c, claims, isAdmin)
	if errResp != nil {
		return errResp
	}

	// Start from the stored config so user_id, enabled and allow_media survive
	// a request that does not mention them.
	config := *existingConfig
	applyWorkerConfigRequest(&config, req)

	if err := model.UpdateWorkerConfig(c.Request().Context(), &config); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update worker config", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Worker config updated successfully", config)
}

// DeleteWorkerConfig deletes a worker configuration
func DeleteWorkerConfig(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	existingConfig, errResp := loadOwnedWorkerConfig(c, claims, claims.Role == "admin")
	if errResp != nil {
		return errResp
	}

	if err := model.DeleteWorkerConfig(c.Request().Context(), existingConfig.ID); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to delete worker config", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Worker config deleted successfully", nil)
}

// ToggleWorkerConfig toggles the enabled status of a worker configuration
func ToggleWorkerConfig(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	existingConfig, errResp := loadOwnedWorkerConfig(c, claims, claims.Role == "admin")
	if errResp != nil {
		return errResp
	}

	if err := model.ToggleWorkerConfig(c.Request().Context(), existingConfig.ID); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to toggle worker config", "INTERNAL_ERROR", err.Error())
	}

	// Get updated config
	updatedConfig, err := model.GetWorkerConfigByID(c.Request().Context(), existingConfig.ID)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Worker config toggled but could not be re-read", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Worker config toggled successfully", updatedConfig)
}

// GetAvailableCircles returns list of available circles from instances
func GetAvailableCircles(c echo.Context) error {
	claims := getClaims(c)
	isAdmin := claims != nil && claims.Role == "admin"
	userID := int64(0)
	if claims != nil {
		userID = claims.UserID
	}

	circles, err := model.GetAvailableCircles(c.Request().Context(), userID, isAdmin)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve available circles", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Available circles retrieved successfully", circles)
}

// GetAvailableApplications returns list of available applications from outbox
func GetAvailableApplications(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED", "")
	}

	isAdmin := claims.Role == "admin"
	applications, err := model.GetAvailableApplications(c.Request().Context(), claims.UserID, isAdmin)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve available applications", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Available applications retrieved successfully", applications)
}

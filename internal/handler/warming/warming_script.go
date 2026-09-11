package warming

import (
	"errors"
	"net/http"
	"strconv"

	"charon/internal/handler"
	warmingModel "charon/internal/model/warming"
	warmingService "charon/internal/service/warming"

	"github.com/labstack/echo/v4"
)

// CreateWarmingScript handles POST /warming/scripts
func CreateWarmingScript(c echo.Context) error {
	var req warmingModel.CreateWarmingScriptRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	// Extract user ID from session context
	userID, ok := c.Get("user_id").(int64)
	if !ok {
		return handler.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	script, err := warmingService.CreateWarmingScriptService(&req, userID)
	if err != nil {
		// Handle validation errors
		if errors.Is(err, warmingService.ErrWarmingScriptTitleRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "TITLE_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrWarmingScriptTitleTooLong) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "TITLE_TOO_LONG", "")
		}
		if errors.Is(err, warmingService.ErrWarmingScriptCategoryTooLong) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "CATEGORY_TOO_LONG", "")
		}

		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to create warming script", "CREATE_FAILED", err.Error())
	}

	resp := warmingModel.ToWarmingScriptResponse(*script)
	return handler.SuccessResponse(c, http.StatusOK, "Warming script created successfully", resp)
}

// GetAllWarmingScripts handles GET /warming/scripts
func GetAllWarmingScripts(c echo.Context) error {
	q := c.QueryParam("q")
	category := c.QueryParam("category")

	// Extract user context from session
	userID, ok := c.Get("user_id").(int64)
	if !ok {
		return handler.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	role, ok := c.Get("role").(string)
	if !ok {
		role = "user" // Default to user if role not found
	}
	isAdmin := role == "admin"

	scripts, err := warmingService.GetAllWarmingScriptsService(q, category, userID, isAdmin)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to get warming scripts", "GET_FAILED", err.Error())
	}

	// Convert to response format
	var responses []warmingModel.WarmingScriptResponse
	for _, script := range scripts {
		responses = append(responses, warmingModel.ToWarmingScriptResponse(script))
	}

	return handler.SuccessResponse(c, http.StatusOK, "Warming scripts retrieved successfully", map[string]any{
		"total":   len(responses),
		"scripts": responses,
	})
}

// GetWarmingScriptByID handles GET /warming/scripts/:id
func GetWarmingScriptByID(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid script ID", "INVALID_ID", err.Error())
	}

	script, err := warmingService.GetWarmingScriptByIDService(id)
	if err != nil {
		if errors.Is(err, warmingService.ErrWarmingScriptNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Warming script not found", "NOT_FOUND", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to get warming script", "GET_FAILED", err.Error())
	}

	// Check ownership for non-admin users (allow public/NULL-owned scripts)
	userID, _ := c.Get("user_id").(int64)
	role, _ := c.Get("role").(string)
	if role != "admin" && script.CreatedBy.Valid && script.CreatedBy.Int64 != userID {
		return handler.ErrorResponse(c, http.StatusForbidden, "Access denied", "FORBIDDEN", "")
	}

	resp := warmingModel.ToWarmingScriptResponse(*script)
	return handler.SuccessResponse(c, http.StatusOK, "Warming script retrieved successfully", resp)
}

// UpdateWarmingScript handles PUT /warming/scripts/:id
func UpdateWarmingScript(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid script ID", "INVALID_ID", err.Error())
	}

	var req warmingModel.UpdateWarmingScriptRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireScriptOwner(c, int64(id), cl, "You don't have permission to update this script"); errResp != nil {
		return errResp
	}

	if err := warmingService.UpdateWarmingScriptService(id, &req); err != nil {
		return mapServiceError(c, err, []errorRule{
			{sentinel: warmingService.ErrWarmingScriptTitleRequired, status: http.StatusBadRequest, code: "TITLE_REQUIRED"},
			{sentinel: warmingService.ErrWarmingScriptTitleTooLong, status: http.StatusBadRequest, code: "TITLE_TOO_LONG"},
			{sentinel: warmingService.ErrWarmingScriptCategoryTooLong, status: http.StatusBadRequest, code: "CATEGORY_TOO_LONG"},
			{sentinel: warmingService.ErrWarmingScriptNotFound, status: http.StatusNotFound, message: "Warming script not found", code: "NOT_FOUND"},
		}, "Failed to update warming script", "UPDATE_FAILED")
	}

	return handler.SuccessResponse(c, http.StatusOK, "Warming script updated successfully", map[string]any{
		"id": id,
	})
}

// DeleteWarmingScript handles DELETE /warming/scripts/:id
func DeleteWarmingScript(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid script ID", "INVALID_ID", err.Error())
	}

	// Extract user context from session
	userID, ok := c.Get("user_id").(int64)
	if !ok {
		return handler.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	role, ok := c.Get("role").(string)
	if !ok {
		role = "user"
	}
	isAdmin := role == "admin"

	// RBAC: Check ownership (Skip if admin)
	if !isAdmin {
		isOwner, err := warmingModel.CheckScriptOwnership(id, userID)
		if err != nil || !isOwner {
			return handler.ErrorResponse(c, http.StatusForbidden, "You don't have permission to delete this script", "FORBIDDEN", "")
		}
	}

	err = warmingService.DeleteWarmingScriptService(id)
	if err != nil {
		if errors.Is(err, warmingService.ErrWarmingScriptNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Warming script not found", "NOT_FOUND", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete warming script", "DELETE_FAILED", err.Error())
	}

	return handler.SuccessResponse(c, http.StatusOK, "Warming script deleted successfully", map[string]any{
		"id": id,
	})
}

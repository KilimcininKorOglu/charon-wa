package warming

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"charon/internal/handler"
	warmingModel "charon/internal/model/warming"
	warmingService "charon/internal/service/warming"

	"github.com/labstack/echo/v4"
)

// CreateWarmingTemplate handles POST /warming/templates
func CreateWarmingTemplate(c echo.Context) error {
	var req warmingModel.CreateWarmingTemplateRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	// Extract user ID from session context
	userID, ok := c.Get("user_id").(int64)
	if !ok {
		return handler.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	template, err := warmingService.CreateWarmingTemplateService(&req, userID)
	if err != nil {
		if errors.Is(err, warmingService.ErrTemplateCategoryRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "CATEGORY_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrTemplateNameRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "NAME_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrTemplateStructureInvalid) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "STRUCTURE_INVALID", "")
		}
		if strings.Contains(err.Error(), "actorRole") || strings.Contains(err.Error(), "messageOptions") {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR", "")
		}
		if strings.Contains(err.Error(), "already exists") {
			return handler.ErrorResponse(c, http.StatusConflict, err.Error(), "DUPLICATE_TEMPLATE", "")
		}

		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to create template", "CREATE_FAILED", err.Error())
	}

	resp := warmingModel.ToWarmingTemplateResponse(*template)
	return handler.SuccessResponse(c, http.StatusOK, "Template created successfully", resp)
}

// GetAllWarmingTemplates handles GET /warming/templates
func GetAllWarmingTemplates(c echo.Context) error {
	category := c.QueryParam("category")

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

	templates, err := warmingService.GetAllWarmingTemplatesService(category, userID, isAdmin)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to get templates", "GET_FAILED", err.Error())
	}

	var responses []warmingModel.WarmingTemplateResponse
	for _, template := range templates {
		responses = append(responses, warmingModel.ToWarmingTemplateResponse(template))
	}

	return handler.SuccessResponse(c, http.StatusOK, "Templates retrieved successfully", map[string]any{
		"total":     len(responses),
		"templates": responses,
	})
}

// GetWarmingTemplateByID handles GET /warming/templates/:id
func GetWarmingTemplateByID(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid template ID", "INVALID_ID", err.Error())
	}

	template, err := warmingService.GetWarmingTemplateByIDService(id)
	if err != nil {
		if errors.Is(err, warmingService.ErrTemplateNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Template not found", "NOT_FOUND", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to get template", "GET_FAILED", err.Error())
	}

	// Check ownership for non-admin users (allow public/NULL-owned templates)
	userID, _ := c.Get("user_id").(int64)
	role, _ := c.Get("role").(string)
	if role != "admin" && template.CreatedBy.Valid && template.CreatedBy.Int64 != userID {
		return handler.ErrorResponse(c, http.StatusForbidden, "Access denied", "FORBIDDEN", "")
	}

	resp := warmingModel.ToWarmingTemplateResponse(*template)
	return handler.SuccessResponse(c, http.StatusOK, "Template retrieved successfully", resp)
}

// UpdateWarmingTemplate handles PUT /warming/templates/:id
func UpdateWarmingTemplate(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid template ID", "INVALID_ID", err.Error())
	}

	var req warmingModel.UpdateWarmingTemplateRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireTemplateOwner(c, id, cl, "You don't have permission to update this template"); errResp != nil {
		return errResp
	}

	if err := warmingService.UpdateWarmingTemplateService(id, &req); err != nil {
		return mapServiceError(c, err, templateErrorRules(), "Failed to update template", "UPDATE_FAILED")
	}

	return handler.SuccessResponse(c, http.StatusOK, "Template updated successfully", map[string]any{
		"id": id,
	})
}

// requireTemplateOwner confirms the caller owns the template. An admin passes
// unconditionally. The result is a written ErrorResponse, or nil.
func requireTemplateOwner(c echo.Context, templateID int64, cl caller, denyMessage string) error {
	if cl.IsAdmin {
		return nil
	}

	isOwner, err := warmingModel.CheckTemplateOwnership(templateID, cl.UserID)
	if err != nil || !isOwner {
		return handler.ErrorResponse(c, http.StatusForbidden, denyMessage, "FORBIDDEN", "")
	}
	return nil
}

// templateErrorRules maps the template service's failures onto HTTP responses.
func templateErrorRules() []errorRule {
	return []errorRule{
		{sentinel: warmingService.ErrTemplateCategoryRequired, status: http.StatusBadRequest, code: "CATEGORY_REQUIRED"},
		{sentinel: warmingService.ErrTemplateNameRequired, status: http.StatusBadRequest, code: "NAME_REQUIRED"},
		{sentinel: warmingService.ErrTemplateStructureInvalid, status: http.StatusBadRequest, code: "STRUCTURE_INVALID"},
		{sentinel: warmingService.ErrTemplateNotFound, status: http.StatusNotFound, message: "Template not found", code: "NOT_FOUND"},
		{substring: "actorRole", status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{substring: "messageOptions", status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{substring: "already exists", status: http.StatusConflict, code: "DUPLICATE_TEMPLATE"},
	}
}

// DeleteWarmingTemplate handles DELETE /warming/templates/:id
func DeleteWarmingTemplate(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid template ID", "INVALID_ID", err.Error())
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
		isOwner, err := warmingModel.CheckTemplateOwnership(id, userID)
		if err != nil || !isOwner {
			return handler.ErrorResponse(c, http.StatusForbidden, "You don't have permission to delete this template", "FORBIDDEN", "")
		}
	}

	err = warmingService.DeleteWarmingTemplateService(id)
	if err != nil {
		if errors.Is(err, warmingService.ErrTemplateNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Template not found", "NOT_FOUND", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete template", "DELETE_FAILED", err.Error())
	}

	return handler.SuccessResponse(c, http.StatusOK, "Template deleted successfully", map[string]any{
		"id": id,
	})
}

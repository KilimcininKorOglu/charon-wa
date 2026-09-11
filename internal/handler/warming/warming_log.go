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

// GetAllWarmingLogs handles GET /warming/logs
func GetAllWarmingLogs(c echo.Context) error {
	roomID := c.QueryParam("roomId")
	status := c.QueryParam("status")
	limitStr := c.QueryParam("limit")

	// Anything unparsable or outside 1..500 falls back to the default.
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 500 {
		limit = 100
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}

	logs, err := warmingService.GetAllWarmingLogsService(roomID, status, limit, cl.UserID, cl.IsAdmin)
	if err != nil {
		return mapServiceError(c, err, []errorRule{
			{substring: "invalid status", status: http.StatusBadRequest, code: "INVALID_STATUS"},
			{substring: "invalid room ID", status: http.StatusBadRequest, code: "INVALID_ROOM_ID"},
		}, "Failed to get logs", "GET_FAILED")
	}

	responses := make([]warmingModel.WarmingLogResponse, 0, len(logs))
	for _, log := range logs {
		responses = append(responses, warmingModel.ToWarmingLogResponse(log))
	}

	return handler.SuccessResponse(c, http.StatusOK, "Logs retrieved successfully", map[string]any{
		"total": len(responses),
		"logs":  responses,
	})
}

// GetWarmingLogByID handles GET /warming/logs/:id
func GetWarmingLogByID(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid log ID", "INVALID_ID", err.Error())
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

	log, err := warmingService.GetWarmingLogByIDService(id, userID, isAdmin)
	if err != nil {
		if errors.Is(err, warmingService.ErrLogNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Log not found", "NOT_FOUND", "")
		}
		if strings.Contains(err.Error(), "forbidden") {
			return handler.ErrorResponse(c, http.StatusForbidden, err.Error(), "FORBIDDEN", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to get log", "GET_FAILED", err.Error())
	}

	resp := warmingModel.ToWarmingLogResponse(*log)
	return handler.SuccessResponse(c, http.StatusOK, "Log retrieved successfully", resp)
}

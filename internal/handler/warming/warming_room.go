package warming

import (
	"errors"
	"net/http"
	"strings"

	"charon/internal/handler"
	warmingModel "charon/internal/model/warming"
	"charon/internal/service"
	warmingService "charon/internal/service/warming"

	"github.com/labstack/echo/v4"
)

// CreateWarmingRoom handles POST /warming/rooms
func CreateWarmingRoom(c echo.Context) error {
	var req warmingModel.CreateWarmingRoomRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	// Extract user ID from session context
	userID, ok := c.Get("user_id").(int64)
	if !ok {
		return handler.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	role, _ := c.Get("role").(string)
	isAdmin := role == "admin"

	room, err := warmingService.CreateWarmingRoomService(&req, userID, isAdmin)
	if err != nil {
		if errors.Is(err, warmingService.ErrRoomNameRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "NAME_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrRoomSenderRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "SENDER_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrRoomReceiverRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "RECEIVER_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrRoomScriptRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "SCRIPT_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrRoomIntervalInvalid) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "INTERVAL_INVALID", "")
		}
		if errors.Is(err, warmingService.ErrRoomSameInstance) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "SAME_INSTANCE", "")
		}
		if strings.Contains(err.Error(), "script not found") {
			return handler.ErrorResponse(c, http.StatusNotFound, "Script not found", "SCRIPT_NOT_FOUND", "")
		}

		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to create room", "CREATE_FAILED", err.Error())
	}

	resp := warmingModel.ToWarmingRoomResponse(*room)
	return handler.SuccessResponse(c, http.StatusOK, "Room created successfully", resp)
}

// GetAllWarmingRooms handles GET /warming/rooms
func GetAllWarmingRooms(c echo.Context) error {
	status := c.QueryParam("status")

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

	rooms, err := warmingService.GetAllWarmingRoomsService(status, userID, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "invalid status") {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "INVALID_STATUS", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to get rooms", "GET_FAILED", err.Error())
	}

	var responses []warmingModel.WarmingRoomResponse
	for _, room := range rooms {
		responses = append(responses, warmingModel.ToWarmingRoomResponse(room))
	}

	return handler.SuccessResponse(c, http.StatusOK, "Rooms retrieved successfully", map[string]interface{}{
		"total": len(responses),
		"rooms": responses,
	})
}

// checkRoomOwnership verifies the caller owns the room (admin bypass)
func checkRoomOwnership(c echo.Context, roomID string) error {
	userID, ok := c.Get("user_id").(int64)
	if !ok {
		return handler.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}
	role, _ := c.Get("role").(string)
	if role == "admin" {
		return nil
	}
	isOwner, err := warmingModel.CheckRoomOwnership(roomID, userID)
	if err != nil || !isOwner {
		return handler.ErrorResponse(c, http.StatusForbidden, "You don't have permission to access this room", "FORBIDDEN", "")
	}
	return nil
}

// GetWarmingRoomByID handles GET /warming/rooms/:id
func GetWarmingRoomByID(c echo.Context) error {
	id := c.Param("id")

	if err := checkRoomOwnership(c, id); err != nil {
		return err
	}

	room, err := warmingService.GetWarmingRoomByIDService(id)
	if err != nil {
		if errors.Is(err, warmingService.ErrRoomNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Room not found", "NOT_FOUND", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to get room", "GET_FAILED", err.Error())
	}

	resp := warmingModel.ToWarmingRoomResponse(*room)
	return handler.SuccessResponse(c, http.StatusOK, "Room retrieved successfully", resp)
}

// UpdateWarmingRoom handles PUT /warming/rooms/:id
func UpdateWarmingRoom(c echo.Context) error {
	id := c.Param("id")

	if err := checkRoomOwnership(c, id); err != nil {
		return err
	}

	var req warmingModel.UpdateWarmingRoomRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	userID, _ := c.Get("user_id").(int64)
	role, _ := c.Get("role").(string)
	isAdmin := role == "admin"

	err := warmingService.UpdateWarmingRoomService(id, &req, userID, isAdmin)
	if err != nil {
		if errors.Is(err, warmingService.ErrRoomNameRequired) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "NAME_REQUIRED", "")
		}
		if errors.Is(err, warmingService.ErrRoomIntervalInvalid) {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "INTERVAL_INVALID", "")
		}
		if errors.Is(err, warmingService.ErrRoomNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Room not found", "NOT_FOUND", "")
		}

		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to update room", "UPDATE_FAILED", err.Error())
	}

	return handler.SuccessResponse(c, http.StatusOK, "Room updated successfully", map[string]interface{}{
		"id": id,
	})
}

// DeleteWarmingRoom handles DELETE /warming/rooms/:id
func DeleteWarmingRoom(c echo.Context) error {
	id := c.Param("id")

	if err := checkRoomOwnership(c, id); err != nil {
		return err
	}

	err := warmingService.DeleteWarmingRoomService(id)
	if err != nil {
		if errors.Is(err, warmingService.ErrRoomNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Room not found", "NOT_FOUND", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete room", "DELETE_FAILED", err.Error())
	}

	// Cleanup auto-reply state for deleted room
	service.CleanupReplyTime(id)

	return handler.SuccessResponse(c, http.StatusOK, "Room deleted successfully", map[string]interface{}{
		"id": id,
	})
}

// UpdateRoomStatus handles PATCH /warming/rooms/:id/status
func UpdateRoomStatus(c echo.Context) error {
	id := c.Param("id")

	if err := checkRoomOwnership(c, id); err != nil {
		return err
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	err := warmingService.UpdateRoomStatusService(id, req.Status)
	if err != nil {
		if errors.Is(err, warmingService.ErrRoomNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Room not found", "NOT_FOUND", "")
		}
		if strings.Contains(err.Error(), "invalid status") {
			return handler.ErrorResponse(c, http.StatusBadRequest, err.Error(), "INVALID_STATUS", "")
		}
		if strings.Contains(err.Error(), "already in") {
			return handler.ErrorResponse(c, http.StatusConflict, err.Error(), "ALREADY_IN_STATUS", "")
		}

		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to update room status", "UPDATE_STATUS_FAILED", err.Error())
	}

	// Clear auto-reply cooldown when room leaves ACTIVE state so a later
	// restart does not inherit stale per-message throttling.
	if req.Status == "FINISHED" || req.Status == "STOPPED" || req.Status == "PAUSED" {
		service.CleanupReplyTime(id)
	}

	return handler.SuccessResponse(c, http.StatusOK, "Room status updated successfully", map[string]interface{}{
		"id":     id,
		"status": req.Status,
	})
}

// RestartWarmingRoom handles POST /warming/rooms/:id/restart
func RestartWarmingRoom(c echo.Context) error {
	id := c.Param("id")

	if err := checkRoomOwnership(c, id); err != nil {
		return err
	}

	err := warmingService.RestartRoomService(id)
	if err != nil {
		if errors.Is(err, warmingService.ErrRoomNotFound) {
			return handler.ErrorResponse(c, http.StatusNotFound, "Room not found", "NOT_FOUND", "")
		}
		return handler.ErrorResponse(c, http.StatusInternalServerError, "Failed to restart room", "RESTART_FAILED", err.Error())
	}

	// Reset auto-reply cooldown so the restarted room starts fresh.
	service.CleanupReplyTime(id)

	return handler.SuccessResponse(c, http.StatusOK, "Room restarted successfully", map[string]interface{}{
		"id":               id,
		"current_sequence": 0,
		"status":           "ACTIVE",
	})
}

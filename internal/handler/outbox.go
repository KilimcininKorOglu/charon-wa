package handler

import (
	"net/http"
	"strconv"

	"hermeswa/internal/model"

	"github.com/labstack/echo/v4"
)

type enqueueRequest struct {
	Destination string `json:"destination"`
	Message     string `json:"message"`
	Application string `json:"application"`
	Type        int    `json:"type"`
	Priority    int    `json:"priority"`
	TableID     string `json:"table_id"`
	File        string `json:"file"`
}

type enqueueBatchRequest struct {
	Messages []enqueueRequest `json:"messages"`
}

// EnqueueOutbox inserts a single message into the outbox queue
func EnqueueOutbox(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	var req enqueueRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	if req.Destination == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Destination is required", "VALIDATION_ERROR", "")
	}
	if req.Message == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Message is required", "VALIDATION_ERROR", "")
	}

	// If API key has a fixed application, enforce it
	req.Application = resolveApplication(c, req.Application)

	msg := model.OutboxEnqueueRequest{
		Destination: req.Destination,
		Message:     req.Message,
		Application: req.Application,
		Type:        req.Type,
		Priority:    req.Priority,
		TableID:     req.TableID,
		File:        req.File,
	}

	id, err := model.EnqueueOutboxMessage(c.Request().Context(), msg, int(claims.UserID))
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to enqueue message", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusCreated, "Message enqueued", map[string]interface{}{
		"id_outbox": id,
	})
}

// EnqueueOutboxBatch inserts multiple messages into the outbox queue
func EnqueueOutboxBatch(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	var req enqueueBatchRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	if len(req.Messages) == 0 {
		return ErrorResponse(c, http.StatusBadRequest, "At least one message is required", "VALIDATION_ERROR", "")
	}
	if len(req.Messages) > 1000 {
		return ErrorResponse(c, http.StatusBadRequest, "Maximum 1000 messages per batch", "VALIDATION_ERROR", "")
	}

	msgs := make([]model.OutboxEnqueueRequest, 0, len(req.Messages))
	for i, m := range req.Messages {
		if m.Destination == "" {
			return ErrorResponse(c, http.StatusBadRequest, "Destination is required for all messages", "VALIDATION_ERROR",
				"message index "+strconv.Itoa(i)+" has empty destination")
		}
		if m.Message == "" {
			return ErrorResponse(c, http.StatusBadRequest, "Message is required for all messages", "VALIDATION_ERROR",
				"message index "+strconv.Itoa(i)+" has empty message")
		}
		msgs = append(msgs, model.OutboxEnqueueRequest{
			Destination: m.Destination,
			Message:     m.Message,
			Application: resolveApplication(c, m.Application),
			Type:        m.Type,
			Priority:    m.Priority,
			TableID:     m.TableID,
			File:        m.File,
		})
	}

	ids, err := model.EnqueueOutboxBatch(c.Request().Context(), msgs, int(claims.UserID))
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to enqueue batch", "INTERNAL_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusCreated, "Batch enqueued", map[string]interface{}{
		"ids":   ids,
		"count": len(ids),
	})
}

// GetOutboxStatus returns the status of a single outbox message
func GetOutboxStatus(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid message ID", "BAD_REQUEST", "")
	}

	// Non-admin users can only see their own messages
	clientID := int(claims.UserID)
	if claims.Role == "admin" {
		clientID = 0 // admin sees all
	}

	msg, err := model.GetOutboxMessage(c.Request().Context(), id, clientID)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound, "Message not found", "NOT_FOUND", "")
	}

	return SuccessResponse(c, http.StatusOK, "Message status retrieved", msg)
}

// ListOutboxMessages returns paginated outbox messages with filtering
func ListOutboxMessages(c echo.Context) error {
	claims := getClaims(c)
	if claims == nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	filter := model.OutboxFilter{
		Application: c.QueryParam("application"),
		Page:        1,
		Limit:       50,
	}

	if p, err := strconv.Atoi(c.QueryParam("page")); err == nil && p > 0 {
		filter.Page = p
	}
	if l, err := strconv.Atoi(c.QueryParam("limit")); err == nil && l > 0 {
		filter.Limit = l
	}
	if s, err := strconv.Atoi(c.QueryParam("status")); err == nil {
		filter.Status = &s
	}

	// Non-admin users can only see their own messages
	if claims.Role != "admin" {
		filter.ClientID = int(claims.UserID)
	}

	messages, total, err := model.ListOutboxMessages(c.Request().Context(), filter)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to list messages", "INTERNAL_ERROR", err.Error())
	}

	if messages == nil {
		messages = []model.OutboxMessage{}
	}

	return SuccessResponse(c, http.StatusOK, "Outbox messages retrieved", map[string]interface{}{
		"messages": messages,
		"total":    total,
	})
}

// resolveApplication checks if the API key has a fixed application, otherwise uses the request value
func resolveApplication(c echo.Context, requestApp string) string {
	key, ok := c.Get("api_key").(*model.APIKey)
	if ok && key != nil && key.Application != "" {
		return key.Application
	}
	return requestApp
}

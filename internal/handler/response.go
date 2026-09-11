package handler

import (
	"log"

	"github.com/labstack/echo/v4"
)

// Standard response structure
type APIResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    any        `json:"data,omitempty"`
	Error   *ErrorInfo `json:"error,omitempty"`
}

type ErrorInfo struct {
	Code      string `json:"code,omitempty"`
	Details   string `json:"details,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// Success response helper
func SuccessResponse(c echo.Context, statusCode int, message string, data any) error {
	return c.JSON(statusCode, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// Error response helper
func ErrorResponse(c echo.Context, statusCode int, message string, errorCode string, details string) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	if requestID == "" {
		requestID = c.Request().Header.Get(echo.HeaderXRequestID)
	}

	// Log full details server-side only — never expose internal errors to clients
	log.Printf("Error: request_id=%s status=%d code=%s message=%q details=%q",
		requestID, statusCode, errorCode, message, details)

	response := APIResponse{
		Success: false,
		Message: message,
	}

	if errorCode != "" || requestID != "" {
		response.Error = &ErrorInfo{
			Code:      errorCode,
			RequestID: requestID,
		}
	}

	return c.JSON(statusCode, response)
}

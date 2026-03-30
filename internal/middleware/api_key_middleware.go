package middleware

import (
	"context"
	"net/http"

	"hermeswa/internal/model"
	"hermeswa/internal/service"

	"github.com/labstack/echo/v4"
)

// APIKeyAuthMiddleware validates X-API-Key header and sets user context
func APIKeyAuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			apiKey := c.Request().Header.Get("X-API-Key")
			if apiKey == "" {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "API key required",
					"error": map[string]string{
						"code": "API_KEY_REQUIRED",
					},
				})
			}

			key, err := model.ValidateAPIKey(c.Request().Context(), apiKey)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Invalid or disabled API key",
					"error": map[string]string{
						"code": "INVALID_API_KEY",
					},
				})
			}

			// Update last used timestamp (async, don't block)
			go model.UpdateAPIKeyLastUsed(context.Background(), key.ID)

			// Set same context keys as JWT middleware for handler compatibility
			claims := &service.Claims{
				UserID:   int64(key.UserID),
				Username: key.Username,
				Role:     key.Role,
			}
			c.Set("user_claims", claims)
			c.Set("user_id", claims.UserID)
			c.Set("username", claims.Username)
			c.Set("role", claims.Role)
			c.Set("api_key", key)

			return next(c)
		}
	}
}

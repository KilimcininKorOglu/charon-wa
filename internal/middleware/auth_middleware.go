package middleware

import (
	"context"
	"log"
	"net/http"

	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// SessionOrAPIKeyMiddleware accepts either a session cookie (UI clients) or an
// X-API-Key header (server-to-server clients such as the outbox worker).
// If the X-API-Key header is present, it is validated first; otherwise the
// session cookie path is used. Both paths set identical context keys so
// downstream handlers and middleware (RequireAdmin, RequireInstanceAccess,
// RequireRole, RequirePhoneNumberAccess) behave identically.
func SessionOrAPIKeyMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if apiKey := c.Request().Header.Get("X-API-Key"); apiKey != "" {
				key, err := model.ValidateAPIKey(c.Request().Context(), apiKey)
				if err != nil {
					return c.JSON(http.StatusUnauthorized, map[string]interface{}{
						"success": false,
						"message": "Invalid or disabled API key",
						"error":   map[string]string{"code": "INVALID_API_KEY"},
					})
				}

				go model.UpdateAPIKeyLastUsed(context.Background(), key.ID)

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

			cookie, err := c.Cookie("session")
			if err != nil || cookie.Value == "" {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Authentication required",
					"error":   map[string]string{"code": "UNAUTHORIZED"},
				})
			}

			session, err := service.ValidateSession(cookie.Value)
			if err != nil {
				if err == model.ErrSessionNotFound || err == model.ErrSessionExpired {
					return c.JSON(http.StatusUnauthorized, map[string]interface{}{
						"success": false,
						"message": "Session expired or invalid",
						"error":   map[string]string{"code": "SESSION_EXPIRED"},
					})
				}
				log.Printf("Session validation error: %v", err)
				return c.JSON(http.StatusInternalServerError, map[string]interface{}{
					"success": false,
					"message": "Internal server error",
				})
			}

			go func() {
				if err := model.TouchAuthSession(session.SessionID, service.GetSessionExpiry()); err != nil {
					log.Printf("Failed to touch session: %v", err)
				}
			}()

			claims := &service.Claims{
				UserID:   session.UserID,
				Username: session.Username,
				Role:     session.Role,
			}
			c.Set("user_claims", claims)
			c.Set("user_id", session.UserID)
			c.Set("username", session.Username)
			c.Set("role", session.Role)

			return next(c)
		}
	}
}

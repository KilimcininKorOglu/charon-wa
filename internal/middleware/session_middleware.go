package middleware

import (
	"log"
	"net/http"

	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// SessionAuthMiddleware validates the session cookie and sets user context
func SessionAuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
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
				if err == model.ErrUserInactive {
					return c.JSON(http.StatusUnauthorized, map[string]interface{}{
						"success": false,
						"message": "Account is disabled",
						"error":   map[string]string{"code": "USER_INACTIVE"},
					})
				}
				log.Printf("Session validation error: %v", err)
				return c.JSON(http.StatusInternalServerError, map[string]interface{}{
					"success": false,
					"message": "Internal server error",
				})
			}

			// Async sliding expiry — extend session on each request
			go func() {
				if err := model.TouchAuthSession(session.SessionID, service.GetSessionExpiry()); err != nil {
					log.Printf("Failed to touch session: %v", err)
				}
			}()

			// Set context keys — MUST match jwt_middleware.go and api_key_middleware.go
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

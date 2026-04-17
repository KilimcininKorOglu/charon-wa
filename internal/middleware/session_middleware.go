package middleware

import (
	"log"
	"net/http"
	"sync"
	"time"

	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// Sliding-expiry updates are cheap individually but a busy session can hammer
// the DB on every request. We debounce per-session so at most one touch fires
// every `touchDebounceInterval` per session ID.
const touchDebounceInterval = 5 * time.Minute

var (
	touchDebounceLastHit sync.Map // map[string]time.Time — session ID → last touch
)

// shouldTouchSession returns true if enough time has elapsed since the last
// touch for this session. Records the new timestamp when it returns true.
func shouldTouchSession(sessionID string) bool {
	now := time.Now()
	if prev, ok := touchDebounceLastHit.Load(sessionID); ok {
		if last, ok := prev.(time.Time); ok && now.Sub(last) < touchDebounceInterval {
			return false
		}
	}
	touchDebounceLastHit.Store(sessionID, now)
	return true
}

// ForgetTouchedSession drops a session from the debounce map (call on logout).
func ForgetTouchedSession(sessionID string) {
	touchDebounceLastHit.Delete(sessionID)
}

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

			// Async sliding expiry — extend session on each request, but
			// debounce per-session so we never hammer the DB on chatty clients.
			if shouldTouchSession(session.SessionID) {
				go func(sid string) {
					if err := model.TouchAuthSession(sid, service.GetSessionExpiry()); err != nil {
						log.Printf("Failed to touch session: %v", err)
					}
				}(session.SessionID)
			}

			// Set context keys — MUST match api_key_middleware.go
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

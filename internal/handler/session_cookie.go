package handler

import (
	"net/http"
	"time"

	"charon/config"

	"github.com/labstack/echo/v4"
)

func setSessionCookie(c echo.Context, rawToken string, maxAge time.Duration) {
	// Secure reads COOKIE_SECURE, which defaults to true. It exists so local
	// development over plain http can still log in. HttpOnly and SameSite=Strict
	// are unconditional.
	// #nosec G124
	cookie := &http.Cookie{
		Name:     "session",
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(maxAge.Seconds()),
	}
	c.SetCookie(cookie)
}

func clearSessionCookie(c echo.Context) {
	// Same COOKIE_SECURE reasoning as setSessionCookie.
	// #nosec G124
	cookie := &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
	c.SetCookie(cookie)
}

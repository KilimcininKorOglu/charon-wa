package handler

import (
	"net/http"
	"os"
	"time"

	"github.com/labstack/echo/v4"
)

func setSessionCookie(c echo.Context, rawToken string, maxAge time.Duration) {
	secure := os.Getenv("COOKIE_SECURE") != "false"
	cookie := &http.Cookie{
		Name:     "session",
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(maxAge.Seconds()),
	}
	c.SetCookie(cookie)
}

func clearSessionCookie(c echo.Context) {
	secure := os.Getenv("COOKIE_SECURE") != "false"
	cookie := &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
	c.SetCookie(cookie)
}

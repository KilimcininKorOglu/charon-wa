package handler

import (
	"log"
	"net/http"
	"slices"

	"charon/config"
	"charon/internal/model"
	"charon/internal/ws"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// Gorilla WebSocket upgrader with origin validation — reuses the origin list
// parsed at startup so a restart is required to rotate allowed origins
// (matches the CORS middleware behaviour).
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Non-browser clients (curl, Postman)
		}
		return slices.Contains(config.CorsAllowOrigins, origin)
	},
}

// WebSocketHandler handles WS connections on the /ws route with cookie-based session auth
func WebSocketHandler(hub *ws.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Read session cookie from the HTTP upgrade request
		cookie, err := c.Request().Cookie("session")
		if err != nil || cookie.Value == "" {
			return c.JSON(http.StatusUnauthorized, map[string]any{
				"success": false,
				"message": "Authentication required. Session cookie missing.",
			})
		}

		// Validate session
		session, err := model.GetAuthSessionByToken(cookie.Value)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]any{
				"success": false,
				"message": "Invalid or expired session",
			})
		}

		conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			log.Printf("ws upgrade error: %v", err)
			return err
		}

		client := ws.NewClient(hub, conn, int(session.UserID), session.Role == "admin")
		hub.Register(client)

		go client.WritePump()
		go client.ReadPump()

		return nil
	}
}

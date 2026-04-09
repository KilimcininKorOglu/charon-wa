package handler

import (
	"log"
	"net/http"
	"os"
	"strings"

	"charon/internal/model"
	"charon/internal/ws"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// Gorilla WebSocket upgrader with origin validation
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Non-browser clients (curl, Postman)
		}
		allowedOrigins := os.Getenv("CORS_ALLOW_ORIGINS")
		if allowedOrigins == "" {
			return false
		}
		for _, o := range strings.Split(allowedOrigins, ",") {
			if strings.TrimSpace(o) == origin {
				return true
			}
		}
		return false
	},
}

// CreateWSTicket issues a one-time WebSocket connection ticket
// POST /api/ws/ticket
func CreateWSTicket(c echo.Context) error {
	userID, ok := c.Get("user_id").(int)
	if !ok {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}
	role, _ := c.Get("role").(string)

	ticket, err := model.CreateWSTicket(int64(userID), role)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to create ticket", "TICKET_FAILED", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Ticket created", map[string]string{
		"ticket": ticket,
	})
}

// WebSocketHandler handles WS connections on the /ws route with ticket auth
func WebSocketHandler(hub *ws.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Require one-time ticket via query parameter
		ticket := c.QueryParam("ticket")
		if ticket == "" {
			return c.JSON(http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"message": "Authentication required. Provide ticket as query parameter.",
			})
		}

		// Consume ticket atomically (single-use)
		userID, role, err := model.ConsumeWSTicket(ticket)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"message": "Invalid or expired ticket",
			})
		}

		conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			log.Printf("ws upgrade error: %v", err)
			return err
		}

		client := ws.NewClient(hub, conn, int(userID), role == "admin")
		hub.Register(client)

		go client.WritePump()
		go client.ReadPump()

		return nil
	}
}

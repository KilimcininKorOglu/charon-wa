package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a single WebSocket connection to FE.
type Client struct {
	hub  *Hub
	conn *websocket.Conn

	// Channel for sending events to this client.
	// Write goroutine reads from here and sends to conn.
	send chan WsEvent

	// User identity from session for event filtering
	UserID  int
	IsAdmin bool

	InstanceID string
}

// Hub stores all active clients and handles event broadcasting.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Register / unregister requests from clients.
	register   chan *Client
	unregister chan *Client

	// Broadcast is the event channel that will be sent to all clients.
	broadcast chan WsEvent

	// Mutex in case synchronous access to clients from outside Run() is needed.
	mu sync.RWMutex
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan WsEvent, 256), // small buffer to prevent blocking
	}
}

// Run must be executed in a separate goroutine.
// This loop will:
// - accept new clients (register)
// - remove disconnected clients (unregister)
// - send events to all clients (broadcast)
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				// Filter events by user access
				if !h.shouldDeliverEvent(client, event) {
					continue
				}
				select {
				case client.send <- event:
					// successfully sent to client buffer
				default:
					// if buffer is full, consider client problematic and disconnect
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Register is used by WS handler when a new connection is created.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister is called when WS connection is closed.
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// Publish implements RealtimePublisher.
// Other services just call this to send events to all clients.
// The InstanceID is auto-extracted from event Data for access filtering.
func (h *Hub) Publish(event WsEvent) {
	// Ensure timestamp is set if not already.
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	// Auto-extract InstanceID from Data if not explicitly set
	if event.InstanceID == "" {
		event.InstanceID = extractInstanceID(event.Data)
	}
	h.broadcast <- event
}

// InstanceAccessChecker is a callback to verify user→instance access.
// Set by main to avoid circular dependency between ws and model packages.
type InstanceAccessChecker func(userID int, instanceID string) bool

// instanceAccessChecker is the global checker set at startup.
var instanceAccessChecker InstanceAccessChecker

// SetInstanceAccessChecker configures the access checker used for broadcast filtering.
func SetInstanceAccessChecker(checker InstanceAccessChecker) {
	instanceAccessChecker = checker
}

// shouldDeliverEvent determines if a client should receive this event.
// Admins receive everything. Non-admin users only get events for their instances.
func (h *Hub) shouldDeliverEvent(client *Client, event WsEvent) bool {
	if client.IsAdmin {
		return true
	}
	if event.InstanceID == "" {
		return true // Events without instance scope go to everyone
	}
	if instanceAccessChecker != nil {
		return instanceAccessChecker(client.UserID, event.InstanceID)
	}
	return false // Deny by default if no checker is configured
}

// RealtimePublisher is the interface held by other services
// (whatsapp.go, QR handler) to avoid direct dependency on Hub.
type RealtimePublisher interface {
	Publish(event WsEvent)
	BroadcastToInstance(instanceID string, data map[string]interface{})
}

// NewClient creates a new Client object from a Gorilla WebSocket connection.
func NewClient(hub *Hub, conn *websocket.Conn, userID int, isAdmin bool) *Client {
	return &Client{
		hub:        hub,
		conn:       conn,
		send:       make(chan WsEvent, 256),
		UserID:     userID,
		IsAdmin:    isAdmin,
		InstanceID: "", // default empty, will be set from handler
	}
}

// WritePump is a loop that sends events from the send channel to the WS connection.
// Usually called as a goroutine from the /ws handler.
func (c *Client) WritePump() {
	ticker := time.NewTicker(5 * time.Minute) // Ping every 5 minutes

	defer func() {
		ticker.Stop()
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			// Set a simple deadline to avoid hanging indefinitely.
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			if !ok {
				// Channel closed. The peer may already be gone, so a write error
				// here is expected and the connection closes either way.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Encode WsEvent ke JSON.
			payload, err := json.Marshal(event)
			if err != nil {
				log.Printf("ws: failed to marshal event: %v", err)
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				log.Printf("ws: failed to write message: %v", err)
				return
			}

		case <-ticker.C:
			// Send ping to keep connection alive
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("ws: failed to send ping: %v", err)
				return
			}
			log.Printf("ws: ping sent to instance: %s", c.InstanceID)
		}
	}
}

// ReadPump optional: read loop from client.
// For the initial version, you can just consume and discard,
// or use it to receive subscribe commands, etc.
// If not needed yet, can be kept minimal / empty.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(512)

	_ = c.conn.SetReadDeadline(time.Now().Add(15 * time.Minute))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(15 * time.Minute))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("ws read error: %v", err)
			break
		}
	}
}

// extractInstanceID tries to pull an instance_id from the event data.
// Supports typed event payloads and generic map[string]interface{}.
func extractInstanceID(data interface{}) string {
	switch d := data.(type) {
	case QRGeneratedData:
		return d.InstanceID
	case QRExpiredData:
		return d.InstanceID
	case InstanceStatusChangedData:
		return d.InstanceID
	case InstanceErrorData:
		return d.InstanceID
	case WarmingMessageData:
		return d.SenderInstanceID
	case map[string]interface{}:
		if id, ok := d["instance_id"].(string); ok {
			return id
		}
	}
	return ""
}

// BroadcastToInstance sends a message to clients listening to a specific instance
func (h *Hub) BroadcastToInstance(instanceID string, data map[string]interface{}) {
	event := WsEvent{
		Event:     "incoming_message",
		Timestamp: time.Now(),
		Data:      data,
	}

	// Add RLock to prevent race condition
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.InstanceID == instanceID {
			select {
			case client.send <- event:
				// Successfully sent event to client
			default:
				// Don't delete here to avoid modifying map during iteration
				// Let Hub.Run() handle cleanup of problematic clients
				log.Printf("⚠️ Client buffer full for instance: %s", instanceID)
			}
		}
	}
}

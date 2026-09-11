package ws

import "time"

// Event name constants for consistency between BE and FE.
const (
	EventQRGenerated           = "QR_GENERATED"
	EventQRExpired             = "QR_EXPIRED"
	EventInstanceStatusChanged = "INSTANCE_STATUS_CHANGED"
	EventInstanceError         = "INSTANCE_ERROR"

	EventQRSuccess   = "QR_SUCCESS" // Pairing successful
	EventQRTimeout   = "QR_TIMEOUT"
	EventQRCancelled = "QR_CANCELLED"
	// For future use:
	// EventQRScanned = "QR_SCANNED"

	EventWarmingMessage = "warming_message" // Warming system message
)

// WsEvent is the common envelope for every message sent via WebSocket.
// FE can switch on the Event field, then cast Data to the appropriate type.
type WsEvent struct {
	Event     string    `json:"event"`     // Event name, one of the constants above
	Timestamp time.Time `json:"timestamp"` // Time event was created (UTC)
	Data      any       `json:"data"`      // Event-specific payload

	// InstanceID is used for server-side routing only — not serialized to JSON.
	// Set by Publish callers to enable user-scoped event filtering.
	InstanceID string `json:"-"`
}

// =====================
// Payload per event type
// =====================

// QRGeneratedData is sent when a new QR is successfully generated
// and ready to be scanned for an instance.
type QRGeneratedData struct {
	InstanceID  string    `json:"instance_id"`
	PhoneNumber string    `json:"phone_number,omitempty"` // can be empty if not yet known
	QRData      string    `json:"qr_data"`                // raw QR string (or URL/base64 as needed by FE)
	ExpiresAt   time.Time `json:"expires_at"`             // QR expiration time
}

// QRExpiredData is sent when QR for a specific instance is considered expired.
type QRExpiredData struct {
	InstanceID  string `json:"instance_id"`
	PhoneNumber string `json:"phone_number,omitempty"`
}

// InstanceStatusChangedData is sent when instance connection status changes,
// e.g. due to events.Connected, events.Disconnected, events.LoggedOut.
type InstanceStatusChangedData struct {
	InstanceID     string     `json:"instance_id"`
	PhoneNumber    string     `json:"phone_number,omitempty"`
	Status         string     `json:"status"`                    // "online", "disconnected", "logged_out", etc.
	IsConnected    bool       `json:"is_connected"`              // true if connection is active
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`    // can be nil if never connected
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"` // can be nil if never disconnected
}

// InstanceErrorData is optional, for sending important instance-related errors,
// e.g. login failed, forced logout due to unofficial app, etc.
type InstanceErrorData struct {
	InstanceID  string `json:"instance_id"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Code        string `json:"code"`    // e.g.: "LOGIN_FAILED", "UNOFFICIAL_APP", "QR_CHANNEL_FAILED"
	Message     string `json:"message"` // human readable message
}

// WarmingMessageData is sent when warming worker sends a simulation message
// to be displayed in the live chat frontend.
type WarmingMessageData struct {
	RoomID             string    `json:"room_id"`
	RoomName           string    `json:"room_name"`
	SenderInstanceID   string    `json:"sender_instance_id"`
	ReceiverInstanceID string    `json:"receiver_instance_id"`
	Message            string    `json:"message"`
	SequenceOrder      int       `json:"sequence_order"`
	ActorRole          string    `json:"actor_role"` // "ACTOR_A" or "ACTOR_B"
	Status             string    `json:"status"`     // "SUCCESS" or "FAILED"
	ErrorMessage       string    `json:"error_message,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
}

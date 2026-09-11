package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"maps"
	"math/rand"
	"sync"
	"time"

	"charon/config"
	"charon/database"
	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/ws"

	"go.mau.fi/whatsmeow/store"
	waLog "go.mau.fi/whatsmeow/util/log"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var (
	sessions     = make(map[string]*model.Session)
	sessionsLock sync.RWMutex

	// Track instances currently logging out
	loggingOut     = make(map[string]bool)
	loggingOutLock sync.RWMutex
	Realtime       ws.RealtimePublisher

	// Track reconnect for staggered activation
	reconnectTracker     = make(map[string]time.Time) // instanceID -> disconnect time
	reconnectTrackerLock sync.RWMutex
	lastReconnectTime    time.Time
	lastReconnectLock    sync.RWMutex

	ErrInstanceNotFound       = errors.New("instance not found")
	ErrInstanceStillConnected = errors.New("instance still connected")
)

// Event handler for connection events
func eventHandler(instanceID string) func(evt any) {
	return func(evt any) {
		switch v := evt.(type) {
		case *events.Connected:
			handleConnectedEvent(instanceID)
		case *events.PairSuccess:
			fmt.Println("✓ Pair Success! Instance:", instanceID)
		case *events.LoggedOut:
			handleLoggedOutEvent(instanceID)
		case *events.StreamReplaced:
			fmt.Println("⚠ Stream replaced! Instance:", instanceID)
		case *events.Disconnected:
			handleDisconnectedEvent(instanceID)
		case *events.Message:
			handleMessageEvent(instanceID, v)
		}
	}
}

// isLoggingOut reports whether a deliberate logout is in progress for the instance.
func isLoggingOut(instanceID string) bool {
	loggingOutLock.RLock()
	defer loggingOutLock.RUnlock()
	return loggingOut[instanceID]
}

// activationDelayForReconnect returns how long to wait before activating a
// reconnected instance. When several devices reconnect at once (the internet
// just came back), each one is staggered by 3-8 seconds.
func activationDelayForReconnect(instanceID string) time.Duration {
	reconnectTrackerLock.Lock()
	disconnectTime, wasDisconnected := reconnectTracker[instanceID]
	delete(reconnectTracker, instanceID) // Remove from tracker
	reconnectTrackerLock.Unlock()

	if !wasDisconnected {
		return 0
	}

	lastReconnectLock.Lock()
	defer lastReconnectLock.Unlock()

	var activationDelay time.Duration
	// If another device reconnected within the last 5 seconds,
	// it likely means internet just recovered (mass reconnect)
	if time.Since(lastReconnectTime) < 5*time.Second && !lastReconnectTime.IsZero() {
		// Reconnect jitter, not a secret.
		// #nosec G404
		activationDelay = time.Duration(rand.Intn(6)+3) * time.Second
		fmt.Printf("⏳ Staggered reconnect: delaying activation for %s by %v (disconnected at: %v)\n",
			instanceID, activationDelay, disconnectTime.Format("15:04:05"))
	}

	lastReconnectTime = time.Now()
	return activationDelay
}

// markSessionConnected flips the in-memory session to connected and refreshes
// its JID. It returns the session, or nil when the instance has no session.
func markSessionConnected(instanceID string) *model.Session {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	session, exists := sessions[instanceID]
	if !exists {
		return nil
	}

	session.IsConnected = true
	if session.Client.Store.ID != nil {
		session.JID = session.Client.Store.ID.String()
	}
	fmt.Println("✓ Connected! Instance:", instanceID, "JID:", session.JID)
	return session
}

// publishInstanceStatus broadcasts an instance status change to WS clients.
func publishInstanceStatus(instanceID, phoneNumber, status string, isConnected bool, connectedAt, disconnectedAt *time.Time) {
	if Realtime == nil {
		return
	}
	now := time.Now().UTC()
	Realtime.Publish(ws.WsEvent{
		Event:     ws.EventInstanceStatusChanged,
		Timestamp: now,
		Data: ws.InstanceStatusChangedData{
			InstanceID:     instanceID,
			PhoneNumber:    phoneNumber,
			Status:         status,
			IsConnected:    isConnected,
			ConnectedAt:    connectedAt,
			DisconnectedAt: disconnectedAt,
		},
	})
}

// startHeartbeat replaces any running heartbeat goroutine with a fresh one that
// sends an available presence every 5 minutes until the session disconnects.
func startHeartbeat(session *model.Session, instanceID string) {
	if session.HeartbeatCancel != nil {
		session.HeartbeatCancel()
		fmt.Println("⏹ Stopped previous heartbeat for:", instanceID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	session.HeartbeatCancel = cancel

	go runHeartbeat(ctx, instanceID)
}

// runHeartbeat is the heartbeat goroutine body. It exits on context cancel or
// once the session is gone or no longer connected.
func runHeartbeat(ctx context.Context, instanceID string) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("⏹ Heartbeat stopped (cancelled) for:", instanceID)
			return

		case <-ticker.C:
			sessionsLock.RLock()
			sess, ok := sessions[instanceID]
			sessionsLock.RUnlock()

			if !ok || !sess.IsConnected {
				fmt.Println("⏹ Heartbeat stopped (disconnected) for:", instanceID)
				return
			}

			if err := sess.Client.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
				fmt.Println("⚠ Heartbeat failed for:", instanceID, err)
			} else {
				fmt.Println("💓 Heartbeat sent for:", instanceID)
			}
		}
	}
}

// handleConnectedEvent runs the full activation sequence for a connected instance.
func handleConnectedEvent(instanceID string) {
	if isLoggingOut(instanceID) {
		fmt.Println("⚠ Ignoring reconnect during logout:", instanceID)
		return
	}

	activationDelay := activationDelayForReconnect(instanceID)
	session := markSessionConnected(instanceID)

	if activationDelay > 0 {
		time.Sleep(activationDelay)
	}
	if session == nil {
		return
	}

	// Send presence on connected, for online status on phone
	if err := session.Client.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
		fmt.Println("⚠ Failed to send presence for instance:", instanceID, err)
	} else {
		fmt.Println("✓ Presence sent (Available) for instance:", instanceID)
	}

	if session.Client.Store.ID == nil {
		return
	}

	// Extract phoneNumber from JID (e.g. "905123456789:38@s.whatsapp.net")
	jid := session.Client.Store.ID
	phoneNumber := jid.User // usually already in 905545 format

	platform := "" // if this field exists; can be empty otherwise
	if err := model.UpdateInstanceOnConnected(instanceID, jid.String(), phoneNumber, platform); err != nil {
		fmt.Println("Warning: failed to update instance on connected:", err)
	}

	now := time.Now().UTC()
	publishInstanceStatus(instanceID, phoneNumber, "online", true, &now, nil)
	startHeartbeat(session, instanceID)
}

// closeLoggedOutSession detaches the device store and disconnects the client.
func closeLoggedOutSession(instanceID string) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	session, exists := sessions[instanceID]
	if !exists {
		return
	}

	session.IsConnected = false
	fmt.Println("✗ Logged out! Instance:", instanceID)

	// Delete device store from whatsapp-db
	if session.Client.Store != nil && session.Client.Store.ID != nil {
		if err := database.Container.DeleteDevice(context.Background(), session.Client.Store); err != nil {
			fmt.Println("⚠ Failed to delete device store:", err)
		} else {
			fmt.Println("✓ Device store deleted for:", instanceID)
		}
	}

	session.Client.Disconnect()
}

// handleLoggedOutEvent tears down the session after WhatsApp logged the device out.
func handleLoggedOutEvent(instanceID string) {
	closeLoggedOutSession(instanceID)

	if err := model.UpdateInstanceOnLoggedOut(instanceID); err != nil {
		fmt.Println("Warning: failed to update instance on logged out:", err)
	} else {
		inst, err := model.GetInstanceByInstanceID(instanceID)
		if err != nil {
			fmt.Printf("Failed to get instance by instance ID %s: %v\n", instanceID, err)
		}
		now := time.Now().UTC()
		publishInstanceStatus(instanceID, inst.PhoneNumber.String, "logged_out", false, &inst.ConnectedAt.Time, &now)
	}

	// Remove session from memory
	sessionsLock.Lock()
	delete(sessions, instanceID)
	sessionsLock.Unlock()

	fmt.Println("✓ Session cleanup completed for:", instanceID)
}

// handleDisconnectedEvent records an unplanned disconnect so the next connect
// can be staggered. A disconnect during a deliberate logout is ignored.
func handleDisconnectedEvent(instanceID string) {
	if isLoggingOut(instanceID) {
		return
	}

	fmt.Println("⚠ Disconnected! Instance:", instanceID)

	reconnectTrackerLock.Lock()
	reconnectTracker[instanceID] = time.Now()
	reconnectTrackerLock.Unlock()

	sessionsLock.Lock()
	if session, exists := sessions[instanceID]; exists {
		session.IsConnected = false
	}
	sessionsLock.Unlock()

	if err := model.UpdateInstanceOnDisconnected(instanceID); err != nil {
		fmt.Println("Warning: failed to update instance on disconnected:", err)
	}
}

// extractMessageText pulls the text out of whichever message variant carries it.
func extractMessageText(v *events.Message) string {
	if text := v.Message.GetConversation(); text != "" {
		return text
	}
	// Extended text message (reply, link preview, etc)
	if text := v.Message.GetExtendedTextMessage().GetText(); text != "" {
		return text
	}
	if caption := v.Message.GetImageMessage().GetCaption(); caption != "" {
		return caption
	}
	return v.Message.GetVideoMessage().GetCaption()
}

// resolveSenderNumber maps a linked-device sender (@lid) back to its phone
// number. It returns the raw LID user when the mapping is unavailable.
func resolveSenderNumber(instanceID string, v *events.Message) string {
	senderNumber := v.Info.Sender.User
	if v.Info.Sender.Server != "lid" {
		return senderNumber
	}

	session, err := GetSession(instanceID)
	if err != nil || session.Client == nil {
		return senderNumber
	}

	// Convert LID to Phone Number using whatsmeow's LID store
	phoneJID, err := session.Client.Store.LIDs.GetPNForLID(context.Background(), v.Info.Sender)
	if err == nil && phoneJID.User != "" {
		log.Printf("✅ Resolved LID %s to phone number: %s", v.Info.Sender.User, phoneJID.User)
		return phoneJID.User
	}

	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Printf("⚠️ HUMAN_VS_BOT: Could not resolve LID to phone number")
	log.Printf("👤 Contact Name: %s", v.Info.PushName)
	log.Printf("🔑 LID (Use this for whitelisting): %s", v.Info.Sender.User)
	log.Printf("💡 To enable auto-reply, set whitelisted_number = '%s'", v.Info.Sender.User)
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	return senderNumber
}

// fanOutIncomingMessage delivers the payload to the WebSocket hub and the
// webhook dispatcher, each behind its own feature flag.
func fanOutIncomingMessage(instanceID string, payload map[string]any) {
	if config.EnableWebsocketIncomingMessage && Realtime != nil {
		Realtime.BroadcastToInstance(instanceID, map[string]any{
			"event": "incoming_message",
			"data":  payload,
		})
		fmt.Printf("✓ Message broadcasted to WebSocket listeners for instance: %s\n", instanceID)
	}

	if config.EnableWebhook {
		SendIncomingMessageWebhook(instanceID, payload)
		fmt.Printf("✓ Webhook dispatched for instance: %s\n", instanceID)
	}
}

// handleMessageEvent processes one inbound message. History-sync replays (older
// than 2 minutes) and the instance's own echoes are dropped.
func handleMessageEvent(instanceID string, v *events.Message) {
	if time.Since(v.Info.Timestamp) > 2*time.Minute {
		return
	}
	if v.Info.IsFromMe {
		return
	}

	messageText := extractMessageText(v)
	senderNumber := resolveSenderNumber(instanceID, v)

	if err := HandleIncomingMessage(instanceID, senderNumber, messageText, v.Info.Chat, v.Info.ID, v.Info.Sender.String()); err != nil {
		log.Printf("[HUMAN_VS_BOT] Error handling incoming message: %v", err)
	}

	fanOutIncomingMessage(instanceID, map[string]any{
		"instance_id": instanceID,
		"from":        v.Info.Sender.String(),
		"from_me":     v.Info.IsFromMe,
		"message":     messageText,
		"timestamp":   v.Info.Timestamp.Unix(),
		"is_group":    v.Info.IsGroup,
		"message_id":  v.Info.ID,
		"push_name":   v.Info.PushName,
	})
}

// Load all devices from database and reconnect
func LoadAllDevices() error {
	devices, err := database.Container.GetAllDevices(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	fmt.Printf("Found %d saved devices in database\n", len(devices))

	for i, device := range devices {
		if device.ID == nil {
			continue
		}

		jid := device.ID.String()

		// 1) Get instanceID from custom DB, DO NOT generate new from JID
		inst, err := model.GetInstanceByJID(jid)
		if err != nil {
			fmt.Printf("Failed to get instance for jid %s: %v\n", jid, err)
			continue
		}

		instanceID := inst.InstanceID
		if instanceID == "" {
			fmt.Printf("Empty instanceID for jid %s, skipping\n", jid)
			continue
		}

		// Add random delay between reconnects (except first device)
		if i > 0 {
			// Random delay 3-10 seconds to avoid bot farm pattern
			// Send-pacing jitter, not a secret.
			// #nosec G404
			delaySeconds := rand.Intn(8) + 3 // 3-10 seconds
			fmt.Printf("⏳ Waiting %d seconds before reconnecting next device ...\n", delaySeconds)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		}

		// 2) Create WhatsMeow client and attach event handler with correct instanceID
		client := whatsmeow.NewClient(device, nil)
		client.AddEventHandler(eventHandler(instanceID))

		if err := client.Connect(); err != nil {
			fmt.Printf("Failed to connect device %s: %v\n", jid, err)
			continue
		}

		// 3) Save to sessions map with consistent instanceID key
		sessionsLock.Lock()
		sessions[instanceID] = &model.Session{
			ID:          instanceID,
			JID:         jid,
			Client:      client,
			IsConnected: client.IsConnected(),
		}
		sessionsLock.Unlock()

		// 4) Update status in DB that this instance successfully reconnected
		//    (if client.IsConnected() == true)
		if client.IsConnected() {
			phoneNumber := helper.ExtractPhoneFromJID(jid) // e.g. "905123456789"

			if err := model.UpdateInstanceOnConnected(
				instanceID,
				jid,
				phoneNumber,
				"", // platform temporarily empty
			); err != nil {
				fmt.Printf("Warning: failed to update instance on reconnect %s: %v\n", instanceID, err)
			}
		}

		fmt.Printf("✓ Loaded and connected: %s (instance: %s)\n", jid, instanceID)
	}

	return nil
}

func CreateSession(instanceID string) (*model.Session, error) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	// Check if session already exists
	if _, exists := sessions[instanceID]; exists {
		return nil, fmt.Errorf("session already exists")
	}

	// Randomize OS to avoid uniformity
	osOptions := []string{"Windows", "macOS", "Linux"}
	// The device name and its suffix are cosmetic. Neither authenticates anything.
	// #nosec G404
	randomOS := osOptions[rand.Intn(len(osOptions))]

	// Generate random suffix (4 digit hex) for unique identity
	// #nosec G404
	randomID := fmt.Sprintf("%04x", rand.Intn(0xffff))

	// Combine OS with unique name: "Windows (Charon-a1b2)"
	customOsName := fmt.Sprintf("%s (Charon-%s)", randomOS, randomID)

	// Set Global Device Props (will be used by NewDevice)
	store.DeviceProps.Os = new(customOsName)
	store.DeviceProps.PlatformType = waProto.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.RequireFullSync = new(false)

	// Create new device
	deviceStore := database.Container.NewDevice()

	// Create whatsmeow client
	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Add event handler
	client.AddEventHandler(eventHandler(instanceID))

	// Save session
	session := &model.Session{
		ID:          instanceID,
		Client:      client,
		IsConnected: false,
	}

	sessions[instanceID] = session
	return session, nil
}

func GetSession(instanceID string) (*model.Session, error) {
	sessionsLock.RLock()
	defer sessionsLock.RUnlock()

	session, exists := sessions[instanceID]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}

	return session, nil
}

// Get all sessions
func GetAllSessions() map[string]*model.Session {
	sessionsLock.RLock()
	defer sessionsLock.RUnlock()

	result := make(map[string]*model.Session)
	maps.Copy(result, sessions)

	return result
}

func DeleteSession(instanceID string) error {
	// Mark as currently logging out to prevent auto-reconnect
	loggingOutLock.Lock()
	loggingOut[instanceID] = true
	loggingOutLock.Unlock()

	// Get session
	sessionsLock.Lock()
	session, exists := sessions[instanceID]
	if !exists {
		sessionsLock.Unlock()

		// Clean up flag
		loggingOutLock.Lock()
		delete(loggingOut, instanceID)
		loggingOutLock.Unlock()

		return fmt.Errorf("session not found")
	}

	// Remove from sessions map (memory)
	delete(sessions, instanceID)
	sessionsLock.Unlock()

	// Stop heartbeat goroutine before logout
	if session.HeartbeatCancel != nil {
		session.HeartbeatCancel()
		fmt.Printf("⏹ Heartbeat cancelled for instance: %s\n", instanceID)
	}

	// LOGOUT: Unlink device from WhatsApp
	if session.Client != nil {
		err := session.Client.Logout(context.Background())
		if err != nil {
			fmt.Printf("Warning: Failed to logout from WhatsApp: %v\n", err)
		}
		session.Client.Disconnect()
	}

	// Update instance status in custom DB (not deleted, just status update)
	err := model.UpdateInstanceStatus(instanceID, "logged_out", false, time.Now())
	if err != nil {
		fmt.Printf("Warning: Failed to update instance status in DB: %v\n", err)
	} else {
		if Realtime != nil {
			now := time.Now().UTC()

			inst, err := model.GetInstanceByInstanceID(instanceID)
			if err != nil {
				fmt.Printf("Failed to get instance by instance ID %s: %v\n", instanceID, err)
			}

			data := ws.InstanceStatusChangedData{
				InstanceID:     instanceID,
				PhoneNumber:    inst.PhoneNumber.String,
				Status:         "logged_out",
				IsConnected:    false,
				ConnectedAt:    &inst.ConnectedAt.Time,
				DisconnectedAt: &now,
			}

			evt := ws.WsEvent{
				Event:     ws.EventInstanceStatusChanged,
				Timestamp: now,
				Data:      data,
			}

			Realtime.Publish(evt)
		}
	}

	// Clean up flag
	loggingOutLock.Lock()
	delete(loggingOut, instanceID)
	loggingOutLock.Unlock()

	fmt.Println("✓ Device logged out, session cleared. Instance kept in DB:", instanceID)
	return nil
}

func DeleteInstance(instanceID string) error {
	inst, err := model.GetInstanceByInstanceID(instanceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInstanceNotFound
		}
		return fmt.Errorf("get instance: %w", err)
	}

	if inst.IsConnected || inst.Status == "online" {
		return ErrInstanceStillConnected
	}

	// Optional: clean up in-memory session + whatsmeow store
	sess, err := GetSession(instanceID)
	if err == nil && sess.Client != nil {
		sess.Client.Disconnect()
		// Delete whatsmeow store data
		_ = sess.Client.Store.Delete(context.Background())

		DeleteSessionFromMemory(instanceID)
	}

	if err := model.DeleteInstanceByInstanceID(instanceID); err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}

	InvalidateWebhookCache(instanceID)

	return nil
}

// Delete whatsmeow session
func DeleteSessionFromMemory(instanceID string) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	if _, ok := sessions[instanceID]; ok {
		delete(sessions, instanceID)
		fmt.Println("Session removed from memory:", instanceID)
	}
}

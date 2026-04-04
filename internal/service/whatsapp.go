package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"hermeswa/config"
	"hermeswa/database"
	"hermeswa/internal/helper"
	"hermeswa/internal/model"
	"hermeswa/internal/ws"

	"go.mau.fi/whatsmeow/store"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

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
func eventHandler(instanceID string) func(evt interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {

		case *events.Connected:
			loggingOutLock.RLock()
			isLoggingOut := loggingOut[instanceID]
			loggingOutLock.RUnlock()
			if isLoggingOut {
				fmt.Println("⚠ Ignoring reconnect during logout:", instanceID)
				return
			}

			// Check if this is a reconnect after disconnect
			reconnectTrackerLock.Lock()
			disconnectTime, wasDisconnected := reconnectTracker[instanceID]
			delete(reconnectTracker, instanceID) // Remove from tracker
			reconnectTrackerLock.Unlock()

			// Calculate delay for staggered activation
			var activationDelay time.Duration
			if wasDisconnected {
				// Check if another device recently reconnected
				lastReconnectLock.Lock()
				timeSinceLastReconnect := time.Since(lastReconnectTime)

				// If another device reconnected within the last 5 seconds,
				// it likely means internet just recovered (mass reconnect)
				if timeSinceLastReconnect < 5*time.Second && !lastReconnectTime.IsZero() {
					// Add 3-8 second delay for this device
					activationDelay = time.Duration(rand.Intn(6)+3) * time.Second
					fmt.Printf("⏳ Staggered reconnect: delaying activation for %s by %v (disconnected at: %v)\n",
						instanceID, activationDelay, disconnectTime.Format("15:04:05"))
				}

				lastReconnectTime = time.Now()
				lastReconnectLock.Unlock()
			}

			sessionsLock.Lock()
			session, exists := sessions[instanceID]
			if exists {
				session.IsConnected = true
				if session.Client.Store.ID != nil {
					session.JID = session.Client.Store.ID.String()
				}

				fmt.Println("✓ Connected! Instance:", instanceID, "JID:", session.JID)
			}
			sessionsLock.Unlock()

			// Wait for activation delay (if any)
			if activationDelay > 0 {
				time.Sleep(activationDelay)
			}

			if exists {
				// Send presence on connected, for online status on phone
				if err := session.Client.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
					fmt.Println("⚠ Failed to send presence for instance:", instanceID, err)
				} else {
					fmt.Println("✓ Presence sent (Available) for instance:", instanceID)
				}
			}

			if exists && session.Client.Store.ID != nil {
				// Extract phoneNumber from JID (e.g. "905123456789:38@s.whatsapp.net")
				jid := session.Client.Store.ID
				phoneNumber := jid.User // usually already in 905545 format

				platform := "" // if this field exists; can be empty otherwise
				if err := model.UpdateInstanceOnConnected(
					instanceID,
					jid.String(),
					phoneNumber,
					platform,
				); err != nil {
					fmt.Println("Warning: failed to update instance on connected:", err)
				}

				// After DB update, send WS event
				if Realtime != nil {
					now := time.Now().UTC()
					data := ws.InstanceStatusChangedData{
						InstanceID:     instanceID,
						PhoneNumber:    phoneNumber,
						Status:         "online",
						IsConnected:    true,
						ConnectedAt:    &now,
						DisconnectedAt: nil,
					}

					evt := ws.WsEvent{
						Event:     ws.EventInstanceStatusChanged,
						Timestamp: now,
						Data:      data,
					}

					Realtime.Publish(evt)
				}

				// Stop old heartbeat if exists (prevent multiple goroutines)
				if session.HeartbeatCancel != nil {
					session.HeartbeatCancel()
					fmt.Println("⏹ Stopped previous heartbeat for:", instanceID)
				}

				// Stop old heartbeat if exists (prevent multiple goroutines)
				ctx, cancel := context.WithCancel(context.Background())
				session.HeartbeatCancel = cancel // Store cancel function

				go func(ctx context.Context, instID string) {
					ticker := time.NewTicker(5 * time.Minute)
					defer ticker.Stop()

					for {
						select {
						case <-ctx.Done():
							// Context cancelled - stop goroutine
							fmt.Println("⏹ Heartbeat stopped (cancelled) for:", instID)
							return

						case <-ticker.C:
							// Send heartbeat
							sessionsLock.RLock()
							sess, ok := sessions[instID]
							sessionsLock.RUnlock()

							if !ok || !sess.IsConnected {
								fmt.Println("⏹ Heartbeat stopped (disconnected) for:", instID)
								return
							}

							if err := sess.Client.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
								fmt.Println("⚠ Heartbeat failed for:", instID, err)
							} else {
								fmt.Println("💓 Heartbeat sent for:", instID)
							}
						}
					}
				}(ctx, instanceID)

			}

		case *events.PairSuccess:
			fmt.Println("✓ Pair Success! Instance:", instanceID)

		case *events.LoggedOut:
			sessionsLock.Lock()
			session, exists := sessions[instanceID]
			if exists {
				session.IsConnected = false
				fmt.Println("✗ Logged out! Instance:", instanceID)

				// Delete device store from whatsapp-db
				if session.Client.Store != nil && session.Client.Store.ID != nil {
					err := database.Container.DeleteDevice(context.Background(), session.Client.Store)
					if err != nil {
						fmt.Println("⚠ Failed to delete device store:", err)
					} else {
						fmt.Println("✓ Device store deleted for:", instanceID)
					}
				}

				// Disconnect client
				session.Client.Disconnect()
			}
			sessionsLock.Unlock()

			// Update DB status
			if err := model.UpdateInstanceOnLoggedOut(instanceID); err != nil {
				fmt.Println("Warning: failed to update instance on logged out:", err)
			} else {
				// Send WS event
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

			// Remove session from memory
			sessionsLock.Lock()
			delete(sessions, instanceID)
			sessionsLock.Unlock()

			fmt.Println("✓ Session cleanup completed for:", instanceID)

		case *events.StreamReplaced:
			fmt.Println("⚠ Stream replaced! Instance:", instanceID)

		case *events.Disconnected:
			loggingOutLock.RLock()
			isLoggingOut := loggingOut[instanceID]
			loggingOutLock.RUnlock()
			if !isLoggingOut {
				fmt.Println("⚠ Disconnected! Instance:", instanceID)

				// Record disconnect time for tracking
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

		//Handle incoming messages
		case *events.Message:
			msgTime := v.Info.Timestamp

			// Filter old messages (History Sync)
			// If message is older than 2 minutes, skip
			if time.Since(msgTime) > 2*time.Minute {
				// fmt.Println("Ignoring old message from history sync")
				return
			}

			// Skip messages from self (echo messages)
			// This prevents duplication in Human vs Bot room
			if v.Info.IsFromMe {
				return
			}

			messageText := v.Message.GetConversation()

			// Handle extended text message (reply, link preview, etc)
			if messageText == "" && v.Message.ExtendedTextMessage != nil {
				messageText = v.Message.GetExtendedTextMessage().GetText()
			}

			// Handle image caption
			if messageText == "" && v.Message.ImageMessage != nil {
				messageText = v.Message.GetImageMessage().GetCaption()
			}

			// Handle video caption
			if messageText == "" && v.Message.VideoMessage != nil {
				messageText = v.Message.GetVideoMessage().GetCaption()
			}

			senderNumber := v.Info.Sender.User

			// If message from linked device (@lid), resolve to real phone number
			if v.Info.Sender.Server == "lid" {
				session, err := GetSession(instanceID)
				if err == nil && session.Client != nil {
					ctx := context.Background()

					// Convert LID to Phone Number using whatsmeow's LID store
					phoneJID, err := session.Client.Store.LIDs.GetPNForLID(ctx, v.Info.Sender)
					if err == nil && phoneJID.User != "" {
						senderNumber = phoneJID.User
						log.Printf("✅ Resolved LID %s to phone number: %s", v.Info.Sender.User, senderNumber)
					} else {
						log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
						log.Printf("⚠️ HUMAN_VS_BOT: Could not resolve LID to phone number")
						log.Printf("👤 Contact Name: %s", v.Info.PushName)
						log.Printf("🔑 LID (Use this for whitelisting): %s", v.Info.Sender.User)
						log.Printf("💡 To enable auto-reply, set whitelisted_number = '%s'", v.Info.Sender.User)
						log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
					}
				}
			}

			if err := HandleIncomingMessage(instanceID, senderNumber, messageText, v.Info.Chat, v.Info.ID, v.Info.Sender.String()); err != nil {
				log.Printf("[HUMAN_VS_BOT] Error handling incoming message: %v", err)
			}

			// Prepare Payload (used by WS & Webhook)
			payload := map[string]interface{}{
				"instance_id": instanceID,
				"from":        v.Info.Sender.String(),
				"from_me":     v.Info.IsFromMe,
				"message":     messageText,
				"timestamp":   v.Info.Timestamp.Unix(),
				"is_group":    v.Info.IsGroup,
				"message_id":  v.Info.ID,
				"push_name":   v.Info.PushName,
			}

			// Broadcast to WebSocket (if enabled)
			if config.EnableWebsocketIncomingMessage && Realtime != nil {
				Realtime.BroadcastToInstance(instanceID, map[string]interface{}{
					"event": "incoming_message",
					"data":  payload,
				})
				fmt.Printf("✓ Message broadcasted to WebSocket listeners for instance: %s\n", instanceID)
			}

			// Broadcast to Webhook (if enabled)
			if config.EnableWebhook {
				SendIncomingMessageWebhook(instanceID, payload)
				fmt.Printf("✓ Webhook dispatched for instance: %s\n", instanceID)
			}

		}

	}
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
	randomOS := osOptions[rand.Intn(len(osOptions))]

	// Generate random suffix (4 digit hex) for unique identity
	randomID := fmt.Sprintf("%04x", rand.Intn(0xffff))

	// Combine OS with unique name: "Windows (HERMESWA-a1b2)"
	customOsName := fmt.Sprintf("%s (HERMESWA-%s)", randomOS, randomID)

	// Set Global Device Props (will be used by NewDevice)
	store.DeviceProps.Os = proto.String(customOsName)
	store.DeviceProps.PlatformType = waProto.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.RequireFullSync = proto.Bool(false)

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
	for k, v := range sessions {
		result[k] = v
	}

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

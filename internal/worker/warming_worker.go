package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"charon/internal/helper"
	warmingModel "charon/internal/model/warming"
	"charon/internal/service"
	"charon/internal/ws"
)

// StartWarmingWorker runs the warming worker until ctx is cancelled.
// The caller is expected to invoke it as a goroutine and cancel ctx on shutdown.
func StartWarmingWorker(ctx context.Context, hub ws.RealtimePublisher) {
	log.Println("🤖 Warming Worker started")

	interval := helper.GetEnvAsInt("WARMING_WORKER_INTERVAL_SECONDS", 5)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("🤖 Warming Worker stopping (context cancelled)")
			return
		case <-ticker.C:
			if err := processActiveRooms(hub); err != nil {
				log.Printf("❌ Worker error: %v", err)
			}
		}
	}
}

// processActiveRooms finds and executes active rooms
func processActiveRooms(hub ws.RealtimePublisher) error {
	rooms, err := warmingModel.GetActiveRoomsForWorker(10)
	if err != nil {
		return fmt.Errorf("failed to get active rooms: %w", err)
	}

	for _, room := range rooms {
		// Acquire a per-room session advisory lock so a parallel worker replica
		// cannot claim the same row between query and execution.
		conn, locked, lockErr := warmingModel.TryLockRoom(context.Background(), room.ID)
		if lockErr != nil {
			log.Printf("⚠️ Advisory lock error for room %s: %v", room.Name, lockErr)
			continue
		}
		if !locked {
			// Another worker is already handling this room in this tick.
			continue
		}

		if err := executeRoom(room, hub); err != nil {
			log.Printf("❌ Failed to execute room %s: %v", room.ID, err)
		}
		warmingModel.UnlockRoom(conn, room.ID)
	}

	return nil
}

// finishRoom marks a room as FINISHED once its script has no remaining lines.
func finishRoom(room warmingModel.WarmingRoom, hub ws.RealtimePublisher) error {
	log.Printf("✅ Room %s: Script finished - all lines executed", room.Name)

	if hub != nil {
		finishedLine := warmingModel.WarmingScriptLine{
			SequenceOrder: room.CurrentSequence,
			ActorRole:     "SYSTEM",
		}
		publishWarmingMessageEvent(
			hub,
			room,
			finishedLine,
			room.SenderInstanceID,
			room.ReceiverInstanceID,
			"Script completed - all dialog sequences finished",
			"FINISHED",
			"",
		)
	}

	return warmingModel.FinishRoom(room.ID)
}

// resolveActors maps the line's actor role onto the room's two instances.
func resolveActors(room warmingModel.WarmingRoom, actorRole string) (string, string) {
	if actorRole == "ACTOR_A" {
		return room.SenderInstanceID, room.ReceiverInstanceID
	}
	return room.ReceiverInstanceID, room.SenderInstanceID
}

// isConnectionError reports whether the send failed because the sender session
// is unusable, which must pause the room instead of retrying forever.
func isConnectionError(errMsg string) bool {
	errMsgLow := strings.ToLower(errMsg)
	for _, marker := range []string{"not connected", "session not found", "not logged in"} {
		if strings.Contains(errMsgLow, marker) {
			return true
		}
	}
	return false
}

// pauseRoom stops a room whose sender session is unusable.
func pauseRoom(room warmingModel.WarmingRoom, line warmingModel.WarmingScriptLine, hub ws.RealtimePublisher, senderID, receiverID, errMsg string) {
	log.Printf("⛔ Room %s PAUSED due to connection error: %s", room.Name, errMsg)

	if hub != nil {
		publishWarmingMessageEvent(hub, room, line, senderID, receiverID, "Room PAUSED: "+errMsg, "PAUSED", errMsg)
	}

	if err := warmingModel.UpdateRoomStatus(room.ID.String(), "PAUSED", nil); err != nil {
		log.Printf("⚠️ Failed to pause room %s: %v", room.Name, err)
	}
}

// recordExecution writes the execution log and publishes the realtime event.
func recordExecution(room warmingModel.WarmingRoom, line warmingModel.WarmingScriptLine, hub ws.RealtimePublisher, senderID, receiverID, message, logStatus, errMsg string) {
	var userID int64
	if room.CreatedBy.Valid {
		userID = room.CreatedBy.Int64
	}

	if err := warmingModel.CreateWarmingLog(room.ID, line.ID, senderID, receiverID, message, logStatus, errMsg, "bot", userID); err != nil {
		log.Printf("⚠️ Failed to create log: %v", err)
	}

	if hub != nil {
		publishWarmingMessageEvent(hub, room, line, senderID, receiverID, message, logStatus, errMsg)
	}
}

// advanceRoom schedules the next run. A failed send keeps the current sequence
// so the same line is retried.
func advanceRoom(room warmingModel.WarmingRoom, line warmingModel.WarmingScriptLine, success bool, errMsg string) error {
	nextRunAt := calculateNextRun(room.IntervalMinSeconds, room.IntervalMaxSeconds)

	sequence := room.CurrentSequence
	if success {
		sequence = line.SequenceOrder
	}
	if err := warmingModel.UpdateRoomProgress(room.ID, sequence, nextRunAt); err != nil {
		return fmt.Errorf("failed to update room: %w", err)
	}

	if success {
		log.Printf("✅ Room %s: Sent message (sequence %d)", room.Name, line.SequenceOrder)
		return nil
	}
	log.Printf("❌ Room %s: Failed to send message - %s (will retry)", room.Name, errMsg)
	return nil
}

func executeRoom(room warmingModel.WarmingRoom, hub ws.RealtimePublisher) error {
	line, err := warmingModel.GetNextAvailableScriptLine(room.ScriptID, room.CurrentSequence)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return finishRoom(room, hub)
		}
		return fmt.Errorf("failed to get script line: %w", err)
	}

	message := helper.RenderSpintax(line.MessageContent)
	senderID, receiverID := resolveActors(room, line.ActorRole)

	success, errMsg := sendWhatsAppMessage(senderID, receiverID, message, room.SendRealMessage)

	logStatus := "SUCCESS"
	if !success {
		logStatus = "FAILED"
	}
	recordExecution(room, *line, hub, senderID, receiverID, message, logStatus, errMsg)

	if !success && isConnectionError(errMsg) {
		pauseRoom(room, *line, hub, senderID, receiverID, errMsg)
		return nil
	}

	return advanceRoom(room, *line, success, errMsg)
}

func sendWhatsAppMessage(senderID, receiverID, message string, sendReal bool) (bool, string) {
	if !sendReal {
		log.Printf("🧪 [SIMULATION] %s → %s: [%d chars]", senderID, receiverID, len(message))
		time.Sleep(100 * time.Millisecond)
		return true, ""
	}

	log.Printf("📤 [REAL] Sending: %s → %s: [%d chars]", senderID, receiverID, len(message))

	success, errMsg := service.SendWarmingMessage(senderID, receiverID, message)

	if success {
		log.Printf("✅ Message sent successfully: %s → %s", senderID, receiverID)
	} else {
		log.Printf("❌ Failed to send: %s", errMsg)
	}

	return success, errMsg
}

// calculateNextRun calculates next run time with random interval
func calculateNextRun(minSec, maxSec int) time.Time {
	interval := minSec
	if maxSec > minSec {
		rangeVal := maxSec - minSec + 1
		if rangeVal > 0 {
			// Room execution jitter, not a secret.
			// #nosec G404
			interval = minSec + rand.Intn(rangeVal)
		}
	}
	return time.Now().Add(time.Duration(interval) * time.Second)
}

func publishWarmingMessageEvent(hub ws.RealtimePublisher, room warmingModel.WarmingRoom, line warmingModel.WarmingScriptLine, senderID, receiverID, message, status, errorMsg string) {
	event := ws.WsEvent{
		Event:     ws.EventWarmingMessage,
		Timestamp: time.Now().UTC(),
		Data: ws.WarmingMessageData{
			RoomID:             room.ID.String(),
			RoomName:           room.Name,
			SenderInstanceID:   senderID,
			ReceiverInstanceID: receiverID,
			Message:            message,
			SequenceOrder:      line.SequenceOrder,
			ActorRole:          line.ActorRole,
			Status:             status,
			ErrorMessage:       errorMsg,
			Timestamp:          time.Now().UTC(),
		},
	}

	hub.Publish(event)

	if status == "FINISHED" {
		log.Printf("🎉 Published script finished event: room=%s", room.Name)
	} else {
		log.Printf("📡 Published warming message event: room=%s, sequence=%d, status=%s", room.Name, line.SequenceOrder, status)
	}
}

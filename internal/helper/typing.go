package helper

import (
	"context"
	"math/rand"
	"time"

	"charon/config"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// ApplyTypingDelay simulates human-like typing delay before sending a message.
// It calculates delay based on message length, applies env overrides from config,
// and sends composing/paused presence updates.
func ApplyTypingDelay(client *whatsmeow.Client, recipient types.JID, messageLength int) {
	baseDelay := 2
	typingSpeed := 0.15
	calculatedDelay := baseDelay + int(float64(messageLength)*typingSpeed)

	variationRange := max(int(float64(calculatedDelay)*0.4), 1)
	// Every rand call in this file picks a typing delay. None produces a secret.
	// #nosec G404
	variation := rand.Intn(variationRange) - int(float64(calculatedDelay)*0.2)
	finalDelay := max(min(calculatedDelay+variation, 30), 3)

	// Override with config values (read once at startup, not per-request)
	if config.TypingDelayMin > 0 && config.TypingDelayMax >= config.TypingDelayMin {
		rangeVal := config.TypingDelayMax - config.TypingDelayMin + 1
		if rangeVal > 0 {
			// #nosec G404
			finalDelay = rand.Intn(rangeVal) + config.TypingDelayMin
		}
	}

	// Send composing presence
	_ = client.SendChatPresence(context.Background(), recipient, types.ChatPresenceComposing, types.ChatPresenceMediaText)

	// Wait 70% of delay
	time.Sleep(time.Duration(finalDelay*70/100) * time.Second)

	// Brief pause (30% chance for messages > 50 chars)
	// #nosec G404
	if messageLength > 50 && rand.Intn(100) < 30 {
		_ = client.SendChatPresence(context.Background(), recipient, types.ChatPresencePaused, types.ChatPresenceMediaText)
		// #nosec G404
		time.Sleep(time.Duration(rand.Intn(2)+1) * time.Second)
		_ = client.SendChatPresence(context.Background(), recipient, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	}

	// Wait remaining 30%
	time.Sleep(time.Duration(finalDelay*30/100) * time.Second)
}

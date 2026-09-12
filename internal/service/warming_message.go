package service

import (
	"context"
	"fmt"

	"charon/internal/helper"
	"charon/internal/model"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// SendWarmingMessage sends a WhatsApp message for warming system
// Returns (success bool, error message string)
func SendWarmingMessage(senderInstanceID, receiverInstanceID, message string) (bool, string) {
	// Get sender session
	senderSession, err := GetSession(senderInstanceID)
	if err != nil {
		return false, fmt.Sprintf("sender session not found: %v", err)
	}

	if !senderSession.IsConnected || !senderSession.Client.IsConnected() {
		return false, "sender not connected"
	}

	if senderSession.Client.Store.ID == nil {
		return false, "sender not logged in"
	}

	// Get receiver session to get JID
	receiverSession, err := GetSession(receiverInstanceID)
	if err != nil {
		return false, fmt.Sprintf("receiver session not found: %v", err)
	}

	if receiverSession.JID == "" {
		return false, "receiver JID not found"
	}

	// Parse receiver JID and ensure it has no device part
	recipientJID, err := types.ParseJID(receiverSession.JID)
	if err != nil {
		return false, fmt.Sprintf("invalid receiver JID: %v", err)
	}
	// Clean device part from JID
	recipientJID = types.JID{
		User:   recipientJID.User,
		Server: recipientJID.Server,
	}

	return sendWarmingMessageInternal(senderSession, recipientJID, message)
}

// SendWarmingMessageToPhone sends a WhatsApp message to a phone number
// Returns (success bool, error message string)
func SendWarmingMessageToPhone(senderInstanceID, phoneNumber, message string) (bool, string) {
	senderSession, err := GetSession(senderInstanceID)
	if err != nil {
		return false, fmt.Sprintf("sender session not found: %v", err)
	}

	if !senderSession.IsConnected || !senderSession.Client.IsConnected() {
		return false, "sender not connected"
	}

	if senderSession.Client.Store.ID == nil {
		return false, "sender not logged in"
	}

	recipientJID := types.NewJID(phoneNumber, types.DefaultUserServer)

	return sendWarmingMessageInternal(senderSession, recipientJID, message)
}

// sendWarmingMessageInternal contains the shared logic for sending messages with typing simulation
func sendWarmingMessageInternal(senderSession *model.Session, recipientJID types.JID, message string) (bool, string) {
	ctx := context.Background()

	if !helper.SkipWhatsAppRegistrationCheck() {
		isRegistered, err := senderSession.Client.IsOnWhatsApp(ctx, []string{recipientJID.User})
		if err != nil {
			return false, fmt.Sprintf("failed to verify receiver number: %v", err)
		}

		if len(isRegistered) == 0 || !isRegistered[0].IsIn {
			return false, "receiver phone number is not registered on WhatsApp"
		}
	}

	// Typing delay simulation
	messageLength := len(message)
	helper.ApplyTypingDelay(senderSession.Client, recipientJID, messageLength)

	// Send message
	msg := &waE2E.Message{
		Conversation: &message,
	}

	_, err := senderSession.Client.SendMessage(ctx, recipientJID, msg)
	if err != nil {
		return false, fmt.Sprintf("failed to send message: %v", err)
	}

	return true, ""
}

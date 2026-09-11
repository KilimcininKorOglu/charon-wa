package ai

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"charon/config"

	"google.golang.org/genai"
)

// geminiRequestTimeout caps the wall-clock for any single Gemini call so a hung
// upstream cannot block auto-reply goroutines indefinitely.
const geminiRequestTimeout = 30 * time.Second

// ConversationMessage represents a single message in conversation history
type ConversationMessage struct {
	Sender  string // "human" or "bot"
	Message string
}

// maxOutputTokensLimit caps the token budget handed to the SDK. AI_DEFAULT_MAX_TOKENS
// is an int read from the environment, so an operator can set a value that does not
// fit in the int32 the SDK takes.
const maxOutputTokensLimit = 1 << 20

// clampMaxTokens converts the configured token budget to the int32 the SDK takes.
// A value at or below zero falls back to the limit, because the SDK treats zero as
// "no output".
func clampMaxTokens(maxTokens int) int32 {
	if maxTokens <= 0 || maxTokens > maxOutputTokensLimit {
		return maxOutputTokensLimit
	}
	return int32(maxTokens)
}

// GenerateReply generates an AI response using Gemini (Official SDK)
func GenerateReply(systemPrompt string, conversationHistory []ConversationMessage, temperature float64, maxTokens int) (string, error) {
	// Validate API key
	if config.GeminiAPIKey == "" {
		return "", fmt.Errorf("gemini API key not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), geminiRequestTimeout)
	defer cancel()

	// Create Gemini client
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  config.GeminiAPIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %w", err)
	}

	// Build prompt from conversation history
	var contextParts []string
	if len(conversationHistory) > 0 {
		contextParts = append(contextParts, "Previous conversation:")
		for _, msg := range conversationHistory {
			role := "Customer"
			if msg.Sender == "bot" {
				role = "You"
			}
			contextParts = append(contextParts, fmt.Sprintf("%s: %s", role, msg.Message))
		}
		contextParts = append(contextParts, "\nPlease respond to the customer's last message:")
	}

	prompt := strings.Join(contextParts, "\n")
	if prompt == "" {
		prompt = "Please greet the customer."
	}

	systemInstruction := systemPrompt
	if systemInstruction == "" {
		systemInstruction = "You are a helpful customer service assistant. Be friendly, concise, and professional."
	}

	// Setup parameters and clean model name
	temp := float32(temperature)
	maxTok := clampMaxTokens(maxTokens)
	modelName := strings.TrimPrefix(config.GeminiDefaultModel, "models/")

	// Call Gemini API
	result, err := client.Models.GenerateContent(
		ctx,
		modelName,
		genai.Text(prompt),
		&genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{
					{Text: systemInstruction},
				},
			},
			Temperature:     &temp,
			MaxOutputTokens: maxTok,
		},
	)
	if err != nil {
		return "", fmt.Errorf("gemini SDK error: %w", err)
	}

	// Extract and return result with detailed logging
	if result == nil {
		return "", fmt.Errorf("nil result from Gemini")
	}

	// Check if we have candidates
	if len(result.Candidates) == 0 {
		return "", fmt.Errorf("no candidates in Gemini response")
	}

	// Get first candidate
	candidate := result.Candidates[0]
	if candidate.Content == nil {
		return "", fmt.Errorf("nil content in candidate")
	}

	// Extract text from parts
	var textParts []string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}

	responseText := strings.Join(textParts, " ")
	if responseText == "" {
		return "", fmt.Errorf("empty response from Gemini")
	}

	// Log truncation for operators but never leak the internal notice to the
	// customer-facing reply — downstream callers receive the partial text.
	if candidate.FinishReason == "MAX_TOKENS" {
		log.Printf("[gemini] response truncated by MAX_TOKENS (increase room.AIMaxTokens to avoid)")
	}

	return strings.TrimSpace(responseText), nil
}

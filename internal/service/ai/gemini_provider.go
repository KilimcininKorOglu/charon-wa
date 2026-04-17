package ai

import (
	"context"
	"fmt"
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

// GenerateReply generates an AI response using Gemini (Official SDK)
func GenerateReply(systemPrompt string, conversationHistory []ConversationMessage, temperature float64, maxTokens int) (string, error) {
	// Validate API key
	if config.GeminiAPIKey == "" {
		return "", fmt.Errorf("Gemini API key not configured")
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
	maxTok := int32(maxTokens)
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
		return "", fmt.Errorf("Gemini SDK Error: %w", err)
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

	// Add notification if response was truncated due to token limit
	if candidate.FinishReason == "MAX_TOKENS" {
		responseText += "\n\n_[Response truncated due to reaching maximum token limit. For a more complete answer, please ask a more specific question or contact admin to increase the token limit.]_"
	}

	return strings.TrimSpace(responseText), nil
}

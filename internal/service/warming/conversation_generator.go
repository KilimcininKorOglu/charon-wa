package warming

import (
	"charon/database"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
)

// TemplateLine represents a single line in template
type TemplateLine struct {
	ActorRole      string   `json:"actorRole"`
	MessageType    string   `json:"messageType,omitempty"` // QUESTION, ANSWER, ANSWER_AND_QUESTION, STATEMENT
	MessageOptions []string `json:"messageOptions"`
}

// GetConversationTemplatesFromDB retrieves templates from database by category
func GetConversationTemplatesFromDB(category string) ([]TemplateLine, error) {
	query := `
		SELECT structure
		FROM warming_templates
		WHERE category = $1
		ORDER BY RANDOM()
		LIMIT 1
	`

	var structureJSON []byte
	err := database.AppDB.QueryRow(query, category).Scan(&structureJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: %s", ErrTemplateNotFound, category)
		}
		return nil, fmt.Errorf("failed to query template: %w", err)
	}

	var lines []TemplateLine
	if err := json.Unmarshal(structureJSON, &lines); err != nil {
		return nil, fmt.Errorf("failed to unmarshal template structure: %w", err)
	}

	return lines, nil
}

// GenerateConversationLines generates conversation lines based on template from database.
// Errors are propagated so callers can surface missing-category / malformed-template problems
// instead of silently persisting an unchanged script.
func GenerateConversationLines(category string, lineCount int) ([]TemplateLine, error) {
	templateLines, err := GetConversationTemplatesFromDB(category)
	if err != nil {
		return nil, err
	}
	if len(templateLines) == 0 {
		return nil, fmt.Errorf("%w: %s (empty structure)", ErrTemplateNotFound, category)
	}

	var result []TemplateLine
	templateIndex := 0

	for i := 0; i < lineCount; i++ {
		// Loop through template lines
		if templateIndex >= len(templateLines) {
			templateIndex = 0 // Restart from beginning
		}

		templateLine := templateLines[templateIndex]
		if len(templateLine.MessageOptions) == 0 {
			return nil, fmt.Errorf("template for %q has an entry without messageOptions", category)
		}

		// Random select from message options
		selectedMessage := templateLine.MessageOptions[rand.Intn(len(templateLine.MessageOptions))]

		result = append(result, TemplateLine{
			ActorRole:      templateLine.ActorRole,
			MessageOptions: []string{selectedMessage},
		})

		templateIndex++
	}

	return result, nil
}

// RandomTypingDuration returns random typing duration between 3-7 seconds
func RandomTypingDuration() int {
	return rand.Intn(5) + 3 // 3-7 seconds
}

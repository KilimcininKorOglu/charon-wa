package warming

import (
	"errors"
	"fmt"
	"strings"

	warmingModel "charon/internal/model/warming"
)

var (
	ErrScriptLineActorRoleInvalid       = errors.New("actor_role must be ACTOR_A or ACTOR_B")
	ErrScriptLineMessageContentRequired = errors.New("message_content is required")
	ErrScriptLineSequenceOrderInvalid   = errors.New("sequence_order must be greater than 0")
	ErrScriptLineTypingDurationInvalid  = errors.New("typing_duration_sec must be greater than 0")
	ErrScriptLineNotFound               = errors.New("warming script line not found")
)

// defaultTypingDurationSec is used when the request carries no usable value.
const defaultTypingDurationSec = 3

// validateScriptLinePayload checks the request fields shared by create and
// update.
func validateScriptLinePayload(sequenceOrder int, actorRole, messageContent string) error {
	if sequenceOrder <= 0 {
		return ErrScriptLineSequenceOrderInvalid
	}
	if actorRole != "ACTOR_A" && actorRole != "ACTOR_B" {
		return ErrScriptLineActorRoleInvalid
	}
	if strings.TrimSpace(messageContent) == "" {
		return ErrScriptLineMessageContentRequired
	}
	return nil
}

// normalizeTypingDuration replaces a non-positive duration with the default.
func normalizeTypingDuration(seconds int) int {
	if seconds <= 0 {
		return defaultTypingDurationSec
	}
	return seconds
}

// ensureScriptExists confirms the parent script is present before its lines are
// read or written.
func ensureScriptExists(scriptID int64) error {
	if _, err := warmingModel.GetWarmingScriptByID(int(scriptID)); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return errors.New("script not found")
		}
		return fmt.Errorf("failed to verify script: %w", err)
	}
	return nil
}

// isDuplicateSequenceError reports whether the database rejected the write
// because the sequence_order is already taken.
func isDuplicateSequenceError(err error) bool {
	return strings.Contains(err.Error(), "unique_script_sequence") || strings.Contains(err.Error(), "duplicate")
}

// CreateWarmingScriptLineService creates new script line with validation
func CreateWarmingScriptLineService(scriptID int64, req *warmingModel.CreateWarmingScriptLineRequest) (*warmingModel.WarmingScriptLine, error) {
	if err := validateScriptLinePayload(req.SequenceOrder, req.ActorRole, req.MessageContent); err != nil {
		return nil, err
	}
	req.TypingDurationSec = normalizeTypingDuration(req.TypingDurationSec)

	if err := ensureScriptExists(scriptID); err != nil {
		return nil, err
	}

	line, err := warmingModel.CreateWarmingScriptLine(scriptID, req)
	if err != nil {
		if isDuplicateSequenceError(err) {
			return nil, fmt.Errorf("sequence_order %d already exists for this script", req.SequenceOrder)
		}
		return nil, fmt.Errorf("service: %w", err)
	}

	return line, nil
}

// GetAllWarmingScriptLinesService retrieves all lines for a script
func GetAllWarmingScriptLinesService(scriptID int64) ([]warmingModel.WarmingScriptLine, error) {
	if err := ensureScriptExists(scriptID); err != nil {
		return nil, err
	}

	lines, err := warmingModel.GetAllWarmingScriptLines(scriptID)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return lines, nil
}

// GetWarmingScriptLineByIDService retrieves single line by ID
func GetWarmingScriptLineByIDService(scriptID int64, lineID int64) (*warmingModel.WarmingScriptLine, error) {
	if lineID <= 0 {
		return nil, errors.New("invalid line ID")
	}

	line, err := warmingModel.GetWarmingScriptLineByID(scriptID, lineID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, ErrScriptLineNotFound
		}
		return nil, fmt.Errorf("service: %w", err)
	}

	return line, nil
}

// UpdateWarmingScriptLineService updates existing line with validation
func UpdateWarmingScriptLineService(scriptID int64, lineID int64, req *warmingModel.UpdateWarmingScriptLineRequest) error {
	if lineID <= 0 {
		return errors.New("invalid line ID")
	}

	if err := validateScriptLinePayload(req.SequenceOrder, req.ActorRole, req.MessageContent); err != nil {
		return err
	}
	req.TypingDurationSec = normalizeTypingDuration(req.TypingDurationSec)

	err := warmingModel.UpdateWarmingScriptLine(scriptID, lineID, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrScriptLineNotFound
		}
		if isDuplicateSequenceError(err) {
			return fmt.Errorf("sequence_order %d already exists for this script", req.SequenceOrder)
		}
		return fmt.Errorf("service: %w", err)
	}

	return nil
}

// DeleteWarmingScriptLineService deletes line by ID
func DeleteWarmingScriptLineService(scriptID int64, lineID int64) error {
	if lineID <= 0 {
		return errors.New("invalid line ID")
	}

	err := warmingModel.DeleteWarmingScriptLine(scriptID, lineID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrScriptLineNotFound
		}
		return fmt.Errorf("service: %w", err)
	}

	return nil
}

// GenerateWarmingScriptLinesService generates conversation lines based on template
func GenerateWarmingScriptLinesService(scriptID int64, category string, lineCount int) ([]warmingModel.WarmingScriptLine, error) {
	// Validate line count
	if lineCount <= 0 || lineCount > 100 {
		return nil, errors.New("line_count must be between 1 and 100")
	}

	if err := ensureScriptExists(scriptID); err != nil {
		return nil, err
	}

	startSequence, err := nextSequenceStart(scriptID)
	if err != nil {
		return nil, err
	}

	templateLines, err := GenerateConversationLines(category, lineCount)
	if err != nil {
		return nil, err
	}

	return persistGeneratedLines(scriptID, startSequence, templateLines)
}

// nextSequenceStart returns the first free sequence_order. It uses the maximum
// existing order rather than the line count, so gaps are preserved.
func nextSequenceStart(scriptID int64) (int, error) {
	existingLines, err := warmingModel.GetAllWarmingScriptLines(scriptID)
	if err != nil {
		return 0, fmt.Errorf("failed to get existing lines: %w", err)
	}

	startSequence := 1
	for _, line := range existingLines {
		startSequence = max(startSequence, line.SequenceOrder+1)
	}
	return startSequence, nil
}

// persistGeneratedLines writes the generated conversation to the script.
func persistGeneratedLines(scriptID int64, startSequence int, templateLines []TemplateLine) ([]warmingModel.WarmingScriptLine, error) {
	var createdLines []warmingModel.WarmingScriptLine
	for i, templateLine := range templateLines {
		req := &warmingModel.CreateWarmingScriptLineRequest{
			SequenceOrder:     startSequence + i,
			ActorRole:         templateLine.ActorRole,
			MessageContent:    templateLine.MessageOptions[0], // Already selected in generator
			TypingDurationSec: RandomTypingDuration(),
		}

		line, err := warmingModel.CreateWarmingScriptLine(scriptID, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create line %d: %w", i+1, err)
		}

		createdLines = append(createdLines, *line)
	}

	return createdLines, nil
}

package warming

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	warmingModel "charon/internal/model/warming"
)

var (
	ErrTemplateCategoryRequired = errors.New("category is required")
	ErrTemplateNameRequired     = errors.New("name is required")
	ErrTemplateStructureInvalid = errors.New("structure must be valid JSON array")
	ErrTemplateNotFound         = errors.New("warming template not found")
)

// validTemplateMessageTypes lists the message types a template line may declare.
var validTemplateMessageTypes = map[string]bool{
	"QUESTION":            true,
	"ANSWER":              true,
	"ANSWER_AND_QUESTION": true,
	"STATEMENT":           true,
	"GREETING":            true,
}

// validateTemplateLine checks one structure entry. The line number is 1-based
// so the message matches what the operator sees.
func validateTemplateLine(lineNumber int, line TemplateLine) error {
	if line.ActorRole != "ACTOR_A" && line.ActorRole != "ACTOR_B" {
		return fmt.Errorf("line %d: actorRole must be ACTOR_A or ACTOR_B", lineNumber)
	}
	if len(line.MessageOptions) == 0 {
		return fmt.Errorf("line %d: messageOptions cannot be empty", lineNumber)
	}
	if strings.TrimSpace(line.MessageType) == "" {
		return fmt.Errorf("line %d: messageType is required (QUESTION, ANSWER, ANSWER_AND_QUESTION, or STATEMENT)", lineNumber)
	}
	if !validTemplateMessageTypes[line.MessageType] {
		return fmt.Errorf("line %d: messageType must be QUESTION, ANSWER, ANSWER_AND_QUESTION, STATEMENT, or GREETING", lineNumber)
	}
	return nil
}

// validateTemplatePayload validates the request fields shared by create and
// update.
func validateTemplatePayload(category, name string, structure []byte) error {
	if strings.TrimSpace(category) == "" {
		return ErrTemplateCategoryRequired
	}
	if strings.TrimSpace(name) == "" {
		return ErrTemplateNameRequired
	}

	var lines []TemplateLine
	if err := json.Unmarshal(structure, &lines); err != nil {
		return ErrTemplateStructureInvalid
	}

	for i, line := range lines {
		if err := validateTemplateLine(i+1, line); err != nil {
			return err
		}
	}
	return nil
}

// isDuplicateTemplateError reports whether the database rejected the write
// because the category/name pair already exists.
func isDuplicateTemplateError(err error) bool {
	return strings.Contains(err.Error(), "unique_category_name") || strings.Contains(err.Error(), "duplicate")
}

// CreateWarmingTemplateService creates new template with validation
func CreateWarmingTemplateService(req *warmingModel.CreateWarmingTemplateRequest, userID int64) (*warmingModel.WarmingTemplate, error) {
	if err := validateTemplatePayload(req.Category, req.Name, req.Structure); err != nil {
		return nil, err
	}

	template, err := warmingModel.CreateWarmingTemplate(req, userID)
	if err != nil {
		if isDuplicateTemplateError(err) {
			return nil, fmt.Errorf("template with category '%s' and name '%s' already exists", req.Category, req.Name)
		}
		return nil, fmt.Errorf("service: %w", err)
	}

	return template, nil
}

// GetAllWarmingTemplatesService retrieves all templates with optional filter
func GetAllWarmingTemplatesService(category string, userID int64, isAdmin bool) ([]warmingModel.WarmingTemplate, error) {
	templates, err := warmingModel.GetAllWarmingTemplates(category, userID, isAdmin)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return templates, nil
}

// GetWarmingTemplateByIDService retrieves single template by ID
func GetWarmingTemplateByIDService(id int64) (*warmingModel.WarmingTemplate, error) {
	if id <= 0 {
		return nil, errors.New("invalid template ID")
	}

	template, err := warmingModel.GetWarmingTemplateByID(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, ErrTemplateNotFound
		}
		return nil, fmt.Errorf("service: %w", err)
	}

	return template, nil
}

// UpdateWarmingTemplateService updates existing template with validation
func UpdateWarmingTemplateService(id int64, req *warmingModel.UpdateWarmingTemplateRequest) error {
	if id <= 0 {
		return errors.New("invalid template ID")
	}

	if err := validateTemplatePayload(req.Category, req.Name, req.Structure); err != nil {
		return err
	}

	err := warmingModel.UpdateWarmingTemplate(id, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrTemplateNotFound
		}
		if isDuplicateTemplateError(err) {
			return fmt.Errorf("template with category '%s' and name '%s' already exists", req.Category, req.Name)
		}
		return fmt.Errorf("service: %w", err)
	}

	return nil
}

// DeleteWarmingTemplateService deletes template by ID
func DeleteWarmingTemplateService(id int64) error {
	if id <= 0 {
		return errors.New("invalid template ID")
	}

	err := warmingModel.DeleteWarmingTemplate(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrTemplateNotFound
		}
		return fmt.Errorf("service: %w", err)
	}

	return nil
}

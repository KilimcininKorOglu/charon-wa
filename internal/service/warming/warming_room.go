package warming

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charon/internal/model"
	warmingModel "charon/internal/model/warming"
)

var (
	ErrRoomNameRequired     = errors.New("name is required")
	ErrRoomSenderRequired   = errors.New("sender_instance_id is required")
	ErrRoomReceiverRequired = errors.New("receiver_instance_id is required")
	ErrRoomScriptRequired   = errors.New("script_id is required")
	ErrRoomIntervalInvalid  = errors.New("interval_max_seconds must be >= interval_min_seconds")
	ErrRoomNotFound         = errors.New("warming room not found")
	ErrRoomAlreadyActive    = errors.New("room is already active")
	ErrRoomNotActive        = errors.New("room is not active")
	ErrRoomSameInstance     = errors.New("sender and receiver cannot be the same instance")
)

// Gemini/AI safety bounds: temperatures outside 0..2 produce degenerate output
// and token budgets outside 1..4096 either waste quota or drop completions.
const (
	aiTemperatureMin = 0.0
	aiTemperatureMax = 2.0
	aiMaxTokensMin   = 1
	aiMaxTokensMax   = 4096
)

func clampTemperature(v float64) float64 {
	if v < aiTemperatureMin {
		return aiTemperatureMin
	}
	if v > aiTemperatureMax {
		return aiTemperatureMax
	}
	return v
}

func clampMaxTokens(v int) int {
	if v < aiMaxTokensMin {
		return aiMaxTokensMin
	}
	if v > aiMaxTokensMax {
		return aiMaxTokensMax
	}
	return v
}

// validateRoomType rejects any room type the executor cannot run.
func validateRoomType(roomType string) error {
	if roomType != "BOT_VS_BOT" && roomType != "HUMAN_VS_BOT" {
		return errors.New("invalid room_type: must be 'BOT_VS_BOT' or 'HUMAN_VS_BOT'")
	}
	return nil
}

// validateRoomInstance checks that an instance exists, is online, and that the
// caller may use it. role names the instance in the error text ("sender"/"receiver").
func validateRoomInstance(instanceID, role string, userID int64, isAdmin bool) error {
	instance, err := model.GetInstanceByInstanceID(instanceID)
	if err != nil {
		return fmt.Errorf("%s instance not found: %s", role, instanceID)
	}
	if instance.Status != "online" {
		return fmt.Errorf("%s instance '%s' is not online (status: %s)", role, instanceID, instance.Status)
	}
	if isAdmin {
		return nil
	}
	if _, err := model.CheckUserInstancePermission(userID, instanceID); err != nil {
		return fmt.Errorf("no permission to use %s instance: %s", role, instanceID)
	}
	return nil
}

// normalizeReplyDelays applies the HUMAN_VS_BOT reply delay defaults in place
// and rejects an inverted range.
func normalizeReplyDelays(minDelay, maxDelay *int) error {
	if *minDelay <= 0 {
		*minDelay = 10
	}
	if *maxDelay <= 0 {
		*maxDelay = 60
	}
	if *maxDelay < *minDelay {
		return errors.New("reply_delay_max must be >= reply_delay_min")
	}
	return nil
}

// normalizeIntervals applies the message interval defaults in place and rejects
// an inverted range.
func normalizeIntervals(minSeconds, maxSeconds *int) error {
	if *minSeconds <= 0 {
		*minSeconds = 5
	}
	if *maxSeconds <= 0 {
		*maxSeconds = 15
	}
	if *maxSeconds < *minSeconds {
		return ErrRoomIntervalInvalid
	}
	return nil
}

// verifyScriptAccess confirms the script exists and, for non-admins, that the
// caller owns it. A missing script and a foreign script report the same error
// so ownership cannot be probed.
func verifyScriptAccess(scriptID int, userID int64, isAdmin bool) error {
	if isAdmin {
		if _, err := warmingModel.GetWarmingScriptByID(scriptID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				return errors.New("script not found")
			}
			return fmt.Errorf("failed to verify script: %w", err)
		}
		return nil
	}

	isOwner, err := warmingModel.CheckScriptOwnership(scriptID, userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return errors.New("script not found")
		}
		return fmt.Errorf("failed to verify script ownership: %w", err)
	}
	if !isOwner {
		return errors.New("script not found")
	}
	return nil
}

// validateBotVsBotRoom checks the two instances a BOT_VS_BOT room drives.
func validateBotVsBotRoom(req *warmingModel.CreateWarmingRoomRequest, userID int64, isAdmin bool) error {
	if strings.TrimSpace(req.SenderInstanceID) == "" {
		return ErrRoomSenderRequired
	}
	if strings.TrimSpace(req.ReceiverInstanceID) == "" {
		return ErrRoomReceiverRequired
	}
	if req.SenderInstanceID == req.ReceiverInstanceID {
		return ErrRoomSameInstance
	}
	if err := validateRoomInstance(req.SenderInstanceID, "sender", userID, isAdmin); err != nil {
		return err
	}
	return validateRoomInstance(req.ReceiverInstanceID, "receiver", userID, isAdmin)
}

// validateHumanVsBotRoom checks the bot side of a HUMAN_VS_BOT room and applies
// its reply delay defaults. The human is the receiver, so the receiver instance
// is cleared.
func validateHumanVsBotRoom(req *warmingModel.CreateWarmingRoomRequest, userID int64, isAdmin bool) error {
	if strings.TrimSpace(req.SenderInstanceID) == "" {
		return ErrRoomSenderRequired
	}
	if strings.TrimSpace(req.WhitelistedNumber) == "" {
		return errors.New("whitelisted_number is required for HUMAN_VS_BOT")
	}
	if err := validateRoomInstance(req.SenderInstanceID, "sender", userID, isAdmin); err != nil {
		return err
	}
	if err := normalizeReplyDelays(&req.ReplyDelayMin, &req.ReplyDelayMax); err != nil {
		return err
	}

	req.ReceiverInstanceID = ""
	return nil
}

// validateCreateRoomRequest runs every check the create path needs and applies
// the request defaults in place.
func validateCreateRoomRequest(req *warmingModel.CreateWarmingRoomRequest, userID int64, isAdmin bool) error {
	if strings.TrimSpace(req.Name) == "" {
		return ErrRoomNameRequired
	}

	// Set default room_type if not provided
	if req.RoomType == "" {
		req.RoomType = "BOT_VS_BOT"
	}
	if err := validateRoomType(req.RoomType); err != nil {
		return err
	}

	if req.RoomType == "BOT_VS_BOT" {
		if err := validateBotVsBotRoom(req, userID, isAdmin); err != nil {
			return err
		}
	} else if err := validateHumanVsBotRoom(req, userID, isAdmin); err != nil {
		return err
	}

	if req.ScriptID <= 0 {
		return ErrRoomScriptRequired
	}
	if err := normalizeIntervals(&req.IntervalMinSeconds, &req.IntervalMaxSeconds); err != nil {
		return err
	}
	return verifyScriptAccess(int(req.ScriptID), userID, isAdmin)
}

// CreateWarmingRoomService creates new room with validation
func CreateWarmingRoomService(req *warmingModel.CreateWarmingRoomRequest, userID int64, isAdmin bool) (*warmingModel.WarmingRoom, error) {
	if err := validateCreateRoomRequest(req, userID, isAdmin); err != nil {
		return nil, err
	}

	// Clamp AI parameters to safe bounds before persisting.
	if req.AIEnabled {
		req.AITemperature = clampTemperature(req.AITemperature)
		if req.AIMaxTokens != 0 {
			req.AIMaxTokens = clampMaxTokens(req.AIMaxTokens)
		}
	}

	room, err := warmingModel.CreateWarmingRoom(req, userID)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return room, nil
}

// GetAllWarmingRoomsService retrieves all rooms with optional filter
func GetAllWarmingRoomsService(status string, userID int64, isAdmin bool) ([]warmingModel.WarmingRoom, error) {
	// Validate status if provided
	if status != "" {
		validStatuses := map[string]bool{
			"STOPPED":  true,
			"ACTIVE":   true,
			"PAUSED":   true,
			"FINISHED": true,
		}
		if !validStatuses[status] {
			return nil, fmt.Errorf("invalid status: must be STOPPED, ACTIVE, PAUSED, or FINISHED")
		}
	}

	rooms, err := warmingModel.GetAllWarmingRooms(status, userID, isAdmin)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return rooms, nil
}

// GetWarmingRoomByIDService retrieves single room by ID
func GetWarmingRoomByIDService(id string) (*warmingModel.WarmingRoom, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("invalid room ID")
	}

	room, err := warmingModel.GetWarmingRoomByID(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("service: %w", err)
	}

	return room, nil
}

// resolveUpdateRoomType returns the room type the update must run under.
// room_type is immutable after creation, so an explicit mismatch is rejected
// and an empty value inherits the stored type.
func resolveUpdateRoomType(id, requested string) (string, error) {
	existingRoom, err := warmingModel.GetWarmingRoomByID(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return "", ErrRoomNotFound
		}
		return "", fmt.Errorf("failed to get existing room: %w", err)
	}

	if requested != "" && requested != existingRoom.RoomType {
		return "", errors.New("room_type cannot be changed after creation. Please create a new room instead")
	}
	return existingRoom.RoomType, nil
}

// validateUpdateRoomRequest runs every check the update path needs and applies
// the request defaults in place.
func validateUpdateRoomRequest(id string, req *warmingModel.UpdateWarmingRoomRequest, userID int64, isAdmin bool) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("invalid room ID")
	}
	if strings.TrimSpace(req.Name) == "" {
		return ErrRoomNameRequired
	}

	roomType, err := resolveUpdateRoomType(id, req.RoomType)
	if err != nil {
		return err
	}
	req.RoomType = roomType
	if err := validateRoomType(req.RoomType); err != nil {
		return err
	}

	if req.RoomType == "HUMAN_VS_BOT" {
		if strings.TrimSpace(req.WhitelistedNumber) == "" {
			return errors.New("whitelisted_number is required for HUMAN_VS_BOT")
		}
		if err := normalizeReplyDelays(&req.ReplyDelayMin, &req.ReplyDelayMax); err != nil {
			return err
		}
	}

	if req.ScriptID <= 0 {
		return ErrRoomScriptRequired
	}
	if err := verifyScriptAccess(int(req.ScriptID), userID, isAdmin); err != nil {
		return err
	}
	return normalizeIntervals(&req.IntervalMinSeconds, &req.IntervalMaxSeconds)
}

// UpdateWarmingRoomService updates existing room with validation
func UpdateWarmingRoomService(id string, req *warmingModel.UpdateWarmingRoomRequest, userID int64, isAdmin bool) error {
	if err := validateUpdateRoomRequest(id, req, userID, isAdmin); err != nil {
		return err
	}

	// Clamp AI parameters to safe bounds before persisting.
	if req.AITemperature != nil {
		clamped := clampTemperature(*req.AITemperature)
		req.AITemperature = &clamped
	}
	if req.AIMaxTokens != nil {
		clamped := clampMaxTokens(*req.AIMaxTokens)
		req.AIMaxTokens = &clamped
	}

	if err := warmingModel.UpdateWarmingRoom(id, req); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrRoomNotFound
		}
		return fmt.Errorf("service: %w", err)
	}

	return nil
}

// DeleteWarmingRoomService deletes room by ID
func DeleteWarmingRoomService(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("invalid room ID")
	}

	err := warmingModel.DeleteWarmingRoom(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrRoomNotFound
		}
		return fmt.Errorf("service: %w", err)
	}

	return nil
}

// UpdateRoomStatusService updates room status with validation
func UpdateRoomStatusService(id string, newStatus string) error {
	// Validate status
	validStatuses := map[string]bool{
		"STOPPED":  true,
		"ACTIVE":   true,
		"PAUSED":   true,
		"FINISHED": true,
	}
	if !validStatuses[newStatus] {
		return fmt.Errorf("invalid status: must be STOPPED, ACTIVE, PAUSED, or FINISHED")
	}

	room, err := GetWarmingRoomByIDService(id)
	if err != nil {
		return err
	}

	// Validate status transitions
	if room.Status == newStatus {
		return fmt.Errorf("room is already in %s status", newStatus)
	}

	// Calculate next_run_at for ACTIVE status
	var nextRunAt *time.Time
	if newStatus == "ACTIVE" {
		nextRun := time.Now().Add(time.Duration(room.IntervalMinSeconds) * time.Second)
		nextRunAt = &nextRun
	}

	err = warmingModel.UpdateRoomStatus(id, newStatus, nextRunAt)
	if err != nil {
		return fmt.Errorf("failed to update room status: %w", err)
	}

	return nil
}

// RestartRoomService restarts a room from beginning
func RestartRoomService(id string) error {
	if id == "" {
		return errors.New("invalid room ID")
	}

	// Check if room exists
	_, err := warmingModel.GetWarmingRoomByID(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to verify room: %w", err)
	}

	// Restart room
	err = warmingModel.RestartRoom(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrRoomNotFound
		}
		return fmt.Errorf("service: %w", err)
	}

	return nil
}

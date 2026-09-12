package warming

import (
	"charon/database"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// WarmingLog represents warming_logs table
type WarmingLog struct {
	ID                 int64
	RoomID             uuid.UUID
	ScriptLineID       sql.NullInt64
	SenderInstanceID   string
	ReceiverInstanceID string
	MessageContent     string
	Status             string // SUCCESS, FAILED
	ErrorMessage       sql.NullString
	CreatedBy          sql.NullInt64
	ExecutedAt         time.Time
}

// WarmingLogResponse for JSON response
type WarmingLogResponse struct {
	ID                 int64     `json:"id"`
	RoomID             string    `json:"roomId"`
	ScriptLineID       *int64    `json:"scriptLineId"`
	SenderInstanceID   string    `json:"senderInstanceId"`
	ReceiverInstanceID string    `json:"receiverInstanceId"`
	MessageContent     string    `json:"messageContent"`
	Status             string    `json:"status"`
	ErrorMessage       *string   `json:"errorMessage"`
	CreatedBy          *int64    `json:"createdBy,omitempty"`
	ExecutedAt         time.Time `json:"executedAt"`
}

// warmingLogFilter builds the WHERE/ORDER/LIMIT tail of the log query. A
// non-admin caller only sees their own rows, and may filter by a room they own.
func warmingLogFilter(roomID, status string, limit int, userID int64, isAdmin bool) (string, []any, error) {
	query := ""
	var args []any
	argIndex := 1

	// RBAC: Filter by ownership for non-admin users
	if !isAdmin {
		query += fmt.Sprintf(" AND (created_by = $%d OR created_by IS NULL)", argIndex)
		args = append(args, userID)
		argIndex++
	}

	if roomID != "" {
		roomUUID, err := uuid.Parse(roomID)
		if err != nil {
			return "", nil, fmt.Errorf("invalid room ID format: %w", err)
		}
		// For non-admin users, verify room ownership before filtering by it.
		if !isAdmin {
			isOwner, err := CheckRoomOwnership(roomID, userID)
			if err != nil || !isOwner {
				return "", nil, fmt.Errorf("forbidden: you do not have access to this room")
			}
		}
		query += fmt.Sprintf(" AND room_id = $%d", argIndex)
		args = append(args, roomUUID)
		argIndex++
	}

	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, status)
		argIndex++
	}

	query += " ORDER BY executed_at DESC"

	if limit > 0 {
		// The appended fragment holds only a $N placeholder. The value travels in args.
		// #nosec G202
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, limit)
	}

	return query, args, nil
}

// scanWarmingLog reads one row of the log query.
func scanWarmingLog(rows *sql.Rows) (WarmingLog, error) {
	var log WarmingLog
	err := rows.Scan(
		&log.ID,
		&log.RoomID,
		&log.ScriptLineID,
		&log.SenderInstanceID,
		&log.ReceiverInstanceID,
		&log.MessageContent,
		&log.Status,
		&log.ErrorMessage,
		&log.CreatedBy,
		&log.ExecutedAt,
	)
	return log, err
}

// GetAllWarmingLogs retrieves logs with optional filters
func GetAllWarmingLogs(roomID, status string, limit int, userID int64, isAdmin bool) ([]WarmingLog, error) {
	query := `
		SELECT id, room_id, script_line_id, sender_instance_id, receiver_instance_id,
		       message_content, status, error_message, created_by, executed_at
		FROM warming_logs
		WHERE 1=1
	`

	filter, args, err := warmingLogFilter(roomID, status, limit, userID, isAdmin)
	if err != nil {
		return nil, err
	}
	// filter comes from warmingLogFilter, which only ever emits fixed column
	// names and $N placeholders. Every value travels in args.
	// #nosec G202
	query += filter

	rows, err := database.AppDB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query warming logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var logs []WarmingLog
	for rows.Next() {
		log, err := scanWarmingLog(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan warming log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, rows.Err()
}

// GetWarmingLogByID retrieves single log by ID
func GetWarmingLogByID(id int64) (*WarmingLog, error) {
	query := `
		SELECT id, room_id, script_line_id, sender_instance_id, receiver_instance_id,
		       message_content, status, error_message, created_by, executed_at
		FROM warming_logs
		WHERE id = $1
	`

	log := &WarmingLog{}
	err := database.AppDB.QueryRow(query, id).Scan(
		&log.ID,
		&log.RoomID,
		&log.ScriptLineID,
		&log.SenderInstanceID,
		&log.ReceiverInstanceID,
		&log.MessageContent,
		&log.Status,
		&log.ErrorMessage,
		&log.CreatedBy,
		&log.ExecutedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("warming log not found")
		}
		return nil, fmt.Errorf("failed to get warming log: %w", err)
	}

	return log, nil
}

// ToWarmingLogResponse converts WarmingLog to response format
func ToWarmingLogResponse(log WarmingLog) WarmingLogResponse {
	resp := WarmingLogResponse{
		ID:                 log.ID,
		RoomID:             log.RoomID.String(),
		SenderInstanceID:   log.SenderInstanceID,
		ReceiverInstanceID: log.ReceiverInstanceID,
		MessageContent:     log.MessageContent,
		Status:             log.Status,
		ExecutedAt:         log.ExecutedAt,
	}

	if log.ScriptLineID.Valid {
		resp.ScriptLineID = &log.ScriptLineID.Int64
	}

	if log.ErrorMessage.Valid {
		resp.ErrorMessage = &log.ErrorMessage.String
	}

	if log.CreatedBy.Valid {
		createdBy := log.CreatedBy.Int64
		resp.CreatedBy = &createdBy
	}

	return resp
}

// CreateWarmingLog creates a new warming log entry with sender type
func CreateWarmingLog(roomID uuid.UUID, scriptLineID int64, senderInstanceID, receiverInstanceID, message, status, errorMessage, senderType string, userID int64) error {
	if senderType == "" {
		senderType = "bot" // default to bot for backward compatibility
	}

	query := `
		INSERT INTO warming_logs 
		(room_id, script_line_id, sender_instance_id, receiver_instance_id, message_content, status, error_message, sender_type, created_by, executed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
	`

	// Use NULL for script_line_id if it's 0 (e.g., for human messages or AI replies)
	var scriptLineIDPtr *int64
	if scriptLineID != 0 {
		scriptLineIDPtr = &scriptLineID
	}

	var userIDPtr *int64
	if userID != 0 {
		userIDPtr = &userID
	}

	_, err := database.AppDB.Exec(query, roomID, scriptLineIDPtr, senderInstanceID, receiverInstanceID, message, status, errorMessage, senderType, userIDPtr)
	if err != nil {
		return fmt.Errorf("failed to create warming log: %w", err)
	}

	return nil
}

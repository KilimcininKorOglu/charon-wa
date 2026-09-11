package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"charon/database"
)

type OutboxMessage struct {
	ID              int64      `json:"id_outbox"`
	Type            int        `json:"type"`
	FromNumber      string     `json:"from_number,omitempty"`
	ClientID        int        `json:"client_id,omitempty"`
	Destination     string     `json:"destination"`
	Messages        string     `json:"messages"`
	Status          int        `json:"status"`
	StatusText      string     `json:"status_text"`
	Priority        int        `json:"priority"`
	Application     string     `json:"application,omitempty"`
	SendingDateTime *time.Time `json:"sending_date_time,omitempty"`
	InsertDateTime  time.Time  `json:"insert_date_time"`
	TableID         string     `json:"table_id,omitempty"`
	File            string     `json:"file,omitempty"`
	ErrorCount      int        `json:"error_count"`
	MsgError        string     `json:"msg_error,omitempty"`
}

type OutboxEnqueueRequest struct {
	Destination string `json:"destination"`
	Message     string `json:"message"`
	Application string `json:"application"`
	Type        int    `json:"type"`
	Priority    int    `json:"priority"`
	TableID     string `json:"table_id"`
	File        string `json:"file"`
}

type OutboxFilter struct {
	Application string
	Status      *int
	ClientID    int
	Page        int
	Limit       int
}

func statusText(status int) string {
	switch status {
	case 0:
		return "pending"
	case 1:
		return "sent"
	case 2:
		return "failed"
	case 3:
		return "processing"
	default:
		return "unknown"
	}
}

// EnqueueOutboxMessage inserts a single message into the outbox queue
func EnqueueOutboxMessage(ctx context.Context, msg OutboxEnqueueRequest, clientID int) (int64, error) {
	msgType := msg.Type
	if msgType == 0 {
		msgType = 1
	}

	var id int64
	err := database.OutboxDB.QueryRowContext(ctx,
		`INSERT INTO outbox (destination, messages, status, application, type, priority, table_id, file, client_id, insertDateTime)
		 VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8, NOW())
		 RETURNING id_outbox`,
		msg.Destination, msg.Message, nullStr(msg.Application), msgType, msg.Priority,
		nullStr(msg.TableID), nullStr(msg.File), clientID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to enqueue message: %w", err)
	}
	return id, nil
}

// EnqueueOutboxBatch inserts multiple messages in a single transaction
func EnqueueOutboxBatch(ctx context.Context, msgs []OutboxEnqueueRequest, clientID int) ([]int64, error) {
	tx, err := database.OutboxDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO outbox (destination, messages, status, application, type, priority, table_id, file, client_id, insertDateTime)
		 VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8, NOW())
		 RETURNING id_outbox`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	ids := make([]int64, 0, len(msgs))
	for _, msg := range msgs {
		msgType := msg.Type
		if msgType == 0 {
			msgType = 1
		}
		var id int64
		if err := stmt.QueryRowContext(ctx,
			msg.Destination, msg.Message, nullStr(msg.Application), msgType, msg.Priority,
			nullStr(msg.TableID), nullStr(msg.File), clientID,
		).Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to insert message: %w", err)
		}
		ids = append(ids, id)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}
	return ids, nil
}

// GetOutboxMessage returns a single outbox message by ID
func GetOutboxMessage(ctx context.Context, id int64, clientID int) (*OutboxMessage, error) {
	var msg OutboxMessage
	var fromNum, app, tableID, file, msgErr sql.NullString
	var sendDT sql.NullTime

	query := `SELECT id_outbox, COALESCE(type, 1), from_number, COALESCE(client_id, 0), destination, messages,
	                 status, priority, application, sendingDateTime, insertDateTime, table_id, file, error_count, msg_error
	          FROM outbox WHERE id_outbox = $1`

	args := []any{id}
	if clientID > 0 {
		query += " AND client_id = $2"
		args = append(args, clientID)
	}

	err := database.OutboxDB.QueryRowContext(ctx, query, args...).Scan(
		&msg.ID, &msg.Type, &fromNum, &msg.ClientID, &msg.Destination, &msg.Messages,
		&msg.Status, &msg.Priority, &app, &sendDT, &msg.InsertDateTime, &tableID, &file, &msg.ErrorCount, &msgErr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("message not found")
		}
		return nil, err
	}

	msg.StatusText = statusText(msg.Status)
	if fromNum.Valid {
		msg.FromNumber = fromNum.String
	}
	if app.Valid {
		msg.Application = app.String
	}
	if sendDT.Valid {
		msg.SendingDateTime = &sendDT.Time
	}
	if tableID.Valid {
		msg.TableID = tableID.String
	}
	if file.Valid {
		msg.File = file.String
	}
	if msgErr.Valid {
		msg.MsgError = msgErr.String
	}
	return &msg, nil
}

// outboxFilterClause builds the WHERE clause for a list filter. It returns the
// clause, its positional arguments, and the next free placeholder index.
func outboxFilterClause(filter OutboxFilter) (string, []any, int) {
	where := []string{}
	args := []any{}
	idx := 1

	addCondition := func(column string, value any) {
		where = append(where, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}

	if filter.ClientID > 0 {
		addCondition("client_id", filter.ClientID)
	}
	if filter.Application != "" {
		addCondition("application", filter.Application)
	}
	if filter.Status != nil {
		addCondition("status", *filter.Status)
	}

	if len(where) == 0 {
		return "", args, idx
	}
	return "WHERE " + strings.Join(where, " AND "), args, idx
}

// outboxPageBounds clamps the requested page window to the supported range.
func outboxPageBounds(filter OutboxFilter) (int, int) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	page := max(filter.Page, 1)
	return limit, (page - 1) * limit
}

// scanOutboxMessage reads one row of the list query into an OutboxMessage.
func scanOutboxMessage(rows *sql.Rows) (OutboxMessage, error) {
	var msg OutboxMessage
	var fromNum, app, tableID, file, msgErr sql.NullString
	var sendDT sql.NullTime

	if err := rows.Scan(
		&msg.ID, &msg.Type, &fromNum, &msg.ClientID, &msg.Destination, &msg.Messages,
		&msg.Status, &msg.Priority, &app, &sendDT, &msg.InsertDateTime, &tableID, &file, &msg.ErrorCount, &msgErr,
	); err != nil {
		return OutboxMessage{}, err
	}

	msg.StatusText = statusText(msg.Status)
	msg.FromNumber = fromNum.String
	msg.Application = app.String
	msg.TableID = tableID.String
	msg.File = file.String
	msg.MsgError = msgErr.String
	if sendDT.Valid {
		msg.SendingDateTime = &sendDT.Time
	}
	return msg, nil
}

// ListOutboxMessages returns paginated outbox messages with filtering
func ListOutboxMessages(ctx context.Context, filter OutboxFilter) ([]OutboxMessage, int, error) {
	whereClause, args, idx := outboxFilterClause(filter)

	// Count
	var total int
	countQuery := "SELECT COUNT(*) FROM outbox " + whereClause
	if err := database.OutboxDB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit, offset := outboxPageBounds(filter)
	dataQuery := fmt.Sprintf(
		`SELECT id_outbox, COALESCE(type, 1), from_number, COALESCE(client_id, 0), destination, messages,
		        status, priority, application, sendingDateTime, insertDateTime, table_id, file, error_count, msg_error
		 FROM outbox %s ORDER BY insertDateTime DESC LIMIT $%d OFFSET $%d`,
		whereClause, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := database.OutboxDB.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var messages []OutboxMessage
	for rows.Next() {
		msg, err := scanOutboxMessage(rows)
		if err != nil {
			return nil, 0, err
		}
		messages = append(messages, msg)
	}
	return messages, total, rows.Err()
}

// GetUserOutboxCountToday returns the number of messages a user has enqueued today
func GetUserOutboxCountToday(ctx context.Context, userID int) (int, error) {
	db := database.OutboxDB

	query := `SELECT COUNT(*) FROM outbox WHERE client_id = $1 AND insertdatetime >= CURRENT_DATE`

	var count int
	err := db.QueryRowContext(ctx, query, userID).Scan(&count)
	return count, err
}

// ErrDailyLimitReached is returned when a user has reached their daily outbox limit.
var ErrDailyLimitReached = fmt.Errorf("daily outbox limit reached")

// enqueueCountToday counts today's messages for a user inside an existing transaction.
func enqueueCountToday(ctx context.Context, tx *sql.Tx, userID int) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox WHERE client_id = $1 AND insertdatetime >= CURRENT_DATE`,
		userID,
	).Scan(&count)
	return count, err
}

// EnqueueOutboxMessageWithLimit atomically checks the daily limit and inserts a
// single message. Uses a PostgreSQL advisory lock to prevent TOCTOU races.
func EnqueueOutboxMessageWithLimit(ctx context.Context, msg OutboxEnqueueRequest, clientID int, maxDaily int) (int64, error) {
	tx, err := database.OutboxDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", clientID); err != nil {
		return 0, fmt.Errorf("failed to acquire lock: %w", err)
	}

	count, err := enqueueCountToday(ctx, tx, clientID)
	if err != nil {
		return 0, fmt.Errorf("failed to check daily limit: %w", err)
	}
	if count >= maxDaily {
		return 0, ErrDailyLimitReached
	}

	msgType := msg.Type
	if msgType == 0 {
		msgType = 1
	}
	var id int64
	if err = tx.QueryRowContext(ctx,
		`INSERT INTO outbox (destination, messages, status, application, type, priority, table_id, file, client_id, insertDateTime)
		 VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8, NOW()) RETURNING id_outbox`,
		msg.Destination, msg.Message, nullStr(msg.Application), msgType, msg.Priority,
		nullStr(msg.TableID), nullStr(msg.File), clientID,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("failed to enqueue message: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit: %w", err)
	}
	return id, nil
}

// EnqueueOutboxBatchWithLimit atomically checks the daily limit and inserts
// a batch of messages. Uses a PostgreSQL advisory lock to prevent TOCTOU races.
func EnqueueOutboxBatchWithLimit(ctx context.Context, msgs []OutboxEnqueueRequest, clientID int, maxDaily int) ([]int64, error) {
	tx, err := database.OutboxDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", clientID); err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	count, err := enqueueCountToday(ctx, tx, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to check daily limit: %w", err)
	}
	if count+len(msgs) > maxDaily {
		return nil, ErrDailyLimitReached
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO outbox (destination, messages, status, application, type, priority, table_id, file, client_id, insertDateTime)
		 VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8, NOW()) RETURNING id_outbox`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	ids := make([]int64, 0, len(msgs))
	for _, msg := range msgs {
		msgType := msg.Type
		if msgType == 0 {
			msgType = 1
		}
		var id int64
		if err = stmt.QueryRowContext(ctx,
			msg.Destination, msg.Message, nullStr(msg.Application), msgType, msg.Priority,
			nullStr(msg.TableID), nullStr(msg.File), clientID,
		).Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to insert message: %w", err)
		}
		ids = append(ids, id)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}
	return ids, nil
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

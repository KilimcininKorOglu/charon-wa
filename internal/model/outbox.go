package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"hermeswa/database"
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
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO outbox (destination, messages, status, application, type, priority, table_id, file, client_id, insertDateTime)
		 VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8, NOW())
		 RETURNING id_outbox`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

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

	args := []interface{}{id}
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

// ListOutboxMessages returns paginated outbox messages with filtering
func ListOutboxMessages(ctx context.Context, filter OutboxFilter) ([]OutboxMessage, int, error) {
	where := []string{}
	args := []interface{}{}
	idx := 1

	if filter.ClientID > 0 {
		where = append(where, fmt.Sprintf("client_id = $%d", idx))
		args = append(args, filter.ClientID)
		idx++
	}
	if filter.Application != "" {
		where = append(where, fmt.Sprintf("application = $%d", idx))
		args = append(args, filter.Application)
		idx++
	}
	if filter.Status != nil {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, *filter.Status)
		idx++
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Count
	var total int
	countQuery := "SELECT COUNT(*) FROM outbox " + whereClause
	if err := database.OutboxDB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Page
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

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
	defer rows.Close()

	var messages []OutboxMessage
	for rows.Next() {
		var msg OutboxMessage
		var fromNum, app, tableID, file, msgErr sql.NullString
		var sendDT sql.NullTime

		if err := rows.Scan(
			&msg.ID, &msg.Type, &fromNum, &msg.ClientID, &msg.Destination, &msg.Messages,
			&msg.Status, &msg.Priority, &app, &sendDT, &msg.InsertDateTime, &tableID, &file, &msg.ErrorCount, &msgErr,
		); err != nil {
			return nil, 0, err
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
		messages = append(messages, msg)
	}
	return messages, total, rows.Err()
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

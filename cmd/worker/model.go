package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

type OutboxMessage struct {
	ID              int64          `json:"id_outbox"`
	Destination     string         `json:"destination"`
	Messages        string         `json:"messages"`
	Status          int            `json:"status"`
	Application     string         `json:"application"`
	TableID         sql.NullString `json:"table_id"`
	File            sql.NullString `json:"file"`
	InsertDateTime  time.Time      `json:"insertDateTime"`
	SendingDateTime sql.NullTime   `json:"sendingDateTime"`
	FromNumber      sql.NullString `json:"from_number"`
	MsgError        sql.NullString `json:"msg_error"`
}

type WorkerConfig struct {
	ID                 int            `json:"id"`
	UserID             int            `json:"user_id"`
	WorkerName         string         `json:"worker_name"`
	Circle             string         `json:"circle"`
	Application        string         `json:"application"`
	MessageType        string         `json:"message_type"` // "direct" or "group"
	IntervalSeconds    int            `json:"interval_seconds"`
	IntervalMaxSeconds int            `json:"interval_max_seconds"`
	Enabled            bool           `json:"enabled"`
	AllowMedia         bool           `json:"allow_media"`
	WebhookURL         sql.NullString `json:"webhook_url"`
	WebhookSecret      sql.NullString `json:"webhook_secret"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

func FetchWorkerConfigs(ctx context.Context) ([]WorkerConfig, error) {
	query := `
		SELECT id, user_id, worker_name, circle, application, message_type,
		       interval_seconds, interval_max_seconds, enabled, allow_media, webhook_url, webhook_secret, created_at, updated_at
		FROM outbox_worker_config
		WHERE enabled = true
	`

	rows, err := ConfigDB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []WorkerConfig
	for rows.Next() {
		var config WorkerConfig
		err := rows.Scan(
			&config.ID,
			&config.UserID,
			&config.WorkerName,
			&config.Circle,
			&config.Application,
			&config.MessageType,
			&config.IntervalSeconds,
			&config.IntervalMaxSeconds,
			&config.Enabled,
			&config.AllowMedia,
			&config.WebhookURL,
			&config.WebhookSecret,
			&config.CreatedAt,
			&config.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}

	return configs, rows.Err()
}

// ClaimPendingOutbox atomically claims one pending message scoped to the worker's owner.
// clientID=0 means "admin worker" and matches any client_id (including legacy NULL rows);
// any other value enforces tenant isolation via client_id = $N.
func ClaimPendingOutbox(ctx context.Context, applications []string, clientID int) (*OutboxMessage, error) {
	var args []interface{}
	appFilter := ""
	tenantFilter := ""

	if clientID > 0 {
		args = append(args, clientID)
		tenantFilter = fmt.Sprintf(" AND client_id = $%d", len(args))
	}

	if len(applications) > 0 {
		placeholders := make([]string, len(applications))
		for i, app := range applications {
			args = append(args, app)
			placeholders[i] = fmt.Sprintf("$%d", len(args))
		}
		appFilter = fmt.Sprintf(" AND application IN (%s)", strings.Join(placeholders, ","))
	}

	query := fmt.Sprintf(`
		UPDATE outbox
		SET status = 3, claimed_at = NOW()
		WHERE id_outbox = (
			SELECT id_outbox
			FROM outbox
			WHERE status = 0%s%s
			ORDER BY insertDateTime ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id_outbox, destination, messages, status, application, table_id, file, insertDateTime
	`, tenantFilter, appFilter)

	row := OutboxDB.QueryRowContext(ctx, query, args...)
	var msg OutboxMessage
	err := row.Scan(&msg.ID, &msg.Destination, &msg.Messages, &msg.Status, &msg.Application, &msg.TableID, &msg.File, &msg.InsertDateTime)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// ReapStaleClaims requeues any outbox rows stuck in status=3 for longer than
// maxAge (typically 10 minutes) so crash-left messages self-heal. Returns
// the number of rows that were returned to status=0.
func ReapStaleClaims(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge)
	res, err := OutboxDB.ExecContext(ctx, `
		UPDATE outbox
		SET status = 0, claimed_at = NULL
		WHERE status = 3 AND (claimed_at IS NULL OR claimed_at < $1)
	`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func UpdateOutboxSuccess(ctx context.Context, id int64, fromNumber string) error {
	query := `
		UPDATE outbox
		SET status = 1, sendingDateTime = NOW(), from_number = $1, msg_error = NULL, claimed_at = NULL
		WHERE id_outbox = $2
	`
	res, err := OutboxDB.ExecContext(ctx, query, fromNumber, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("no rows affected for id %d", id)
	}
	return nil
}

func UpdateOutboxFailed(ctx context.Context, id int64, errorMsg string) error {
	query := `
		UPDATE outbox
		SET status = 2, msg_error = $1, error_count = error_count + 1, claimed_at = NULL
		WHERE id_outbox = $2
	`
	res, err := OutboxDB.ExecContext(ctx, query, errorMsg, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("no rows affected for id %d", id)
	}
	return nil
}

func LogWorkerEvent(workerID int, workerName, level, message string) {
	query := `
		INSERT INTO worker_system_logs (worker_id, worker_name, level, message)
		VALUES ($1, $2, $3, $4)
	`
	var wID interface{}
	if workerID > 0 {
		wID = workerID
	}
	_, err := ConfigDB.Exec(query, wID, workerName, level, message)
	if err != nil {
		log.Printf("CRITICAL: Failed to write system log to DB: %v", err)
	}
}

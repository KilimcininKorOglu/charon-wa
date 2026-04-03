// internal/model/audit_log.go
package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"hermeswa/database"
)

// AuditLog represents an audit trail entry for security and compliance
type AuditLog struct {
	ID           int64
	UserID       sql.NullInt64
	Action       string
	ResourceType sql.NullString
	ResourceID   sql.NullString
	Details      map[string]interface{}
	IPAddress    sql.NullString
	UserAgent    sql.NullString
	CreatedAt    time.Time
}

// LogAction creates an audit log entry
func LogAction(log *AuditLog) error {
	db := database.AppDB

	// DEBUG: Check database connection
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Convert details map to JSONB
	var detailsJSON interface{}
	if log.Details != nil && len(log.Details) > 0 {
		jsonBytes, err := json.Marshal(log.Details)
		if err != nil {
			return err
		}
		detailsJSON = jsonBytes
	} else {
		// If details is nil or empty, use NULL instead of empty JSON
		detailsJSON = nil
	}

	query := `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, details, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`

	err := db.QueryRow(
		query,
		log.UserID,
		log.Action,
		log.ResourceType,
		log.ResourceID,
		detailsJSON,
		log.IPAddress,
		log.UserAgent,
	).Scan(&log.ID, &log.CreatedAt)

	if err != nil {
		return err
	}

	return nil
}

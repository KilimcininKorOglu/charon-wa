// internal/model/user_instance.go
package model

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hermeswa/database"
)

// UserInstance represents the relationship between a user and an instance
type UserInstance struct {
	ID              int64
	UserID          int64
	InstanceID      string
	PermissionLevel string // Legacy field (not used for authorization)
	CreatedAt       time.Time
}

var (
	ErrNoPermission           = errors.New("user does not have permission for this instance")
	ErrInsufficientPermission = errors.New("insufficient permission level")
)

// AssignInstanceToUser creates a user-instance relationship
func AssignInstanceToUser(userID int64, instanceID string, permission string) error {
	db := database.AppDB

	query := `
		INSERT INTO user_instances (user_id, instance_id, permission_level)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, instance_id) 
		DO UPDATE SET permission_level = $3
	`

	_, err := db.Exec(query, userID, instanceID, permission)
	return err
}

// GetUserInstances retrieves all instance IDs that a user has access to
func GetUserInstances(userID int64) ([]string, error) {
	db := database.AppDB

	query := `
		SELECT instance_id 
		FROM user_instances 
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instanceIDs []string
	for rows.Next() {
		var instanceID string
		if err := rows.Scan(&instanceID); err != nil {
			return nil, err
		}
		instanceIDs = append(instanceIDs, instanceID)
	}

	return instanceIDs, nil
}

// GetInstanceUsers retrieves all users who have access to an instance
func GetInstanceUsers(instanceID string) ([]UserInstance, error) {
	db := database.AppDB

	query := `
		SELECT id, user_id, instance_id, permission_level, created_at
		FROM user_instances
		WHERE instance_id = $1
		ORDER BY created_at ASC
	`

	rows, err := db.Query(query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userInstances []UserInstance
	for rows.Next() {
		var ui UserInstance
		if err := rows.Scan(&ui.ID, &ui.UserID, &ui.InstanceID, &ui.PermissionLevel, &ui.CreatedAt); err != nil {
			return nil, err
		}
		userInstances = append(userInstances, ui)
	}

	return userInstances, nil
}

// CheckUserInstancePermission checks if a user has permission for an instance
// Returns the permission level if user has access, error otherwise
func CheckUserInstancePermission(userID int64, instanceID string) (string, error) {
	db := database.AppDB

	query := `
		SELECT permission_level 
		FROM user_instances 
		WHERE user_id = $1 AND instance_id = $2
	`

	var permissionLevel string
	err := db.QueryRow(query, userID, instanceID).Scan(&permissionLevel)

	if err == sql.ErrNoRows {
		return "", ErrNoPermission
	}
	if err != nil {
		return "", err
	}

	return permissionLevel, nil
}

// RemoveUserInstance removes a user's access to an instance
func RemoveUserInstance(userID int64, instanceID string) error {
	db := database.AppDB

	query := `DELETE FROM user_instances WHERE user_id = $1 AND instance_id = $2`

	result, err := db.Exec(query, userID, instanceID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNoPermission
	}

	return nil
}

// UpdateInstanceCreatedBy updates the created_by field in instances table
func UpdateInstanceCreatedBy(instanceID string, userID int64) error {
	db := database.AppDB

	query := `UPDATE instances SET created_by = $1 WHERE instance_id = $2`

	_, err := db.Exec(query, userID, instanceID)
	return err
}

// GetUserInstanceCircles returns distinct circles from instances the user has access to
func GetUserInstanceCircles(userID int64) ([]string, error) {
	db := database.AppDB

	query := `
		SELECT DISTINCT i.circle
		FROM user_instances ui
		JOIN instances i ON ui.instance_id = i.instance_id
		WHERE ui.user_id = $1 AND i.circle IS NOT NULL AND i.circle != ''
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var circles []string
	for rows.Next() {
		var circle string
		if err := rows.Scan(&circle); err != nil {
			return nil, err
		}
		circles = append(circles, circle)
	}

	return circles, nil
}

// CountUserInstances returns the number of instances a user owns
func CountUserInstances(userID int64) (int, error) {
	db := database.AppDB

	query := `SELECT COUNT(*) FROM user_instances WHERE user_id = $1`

	var count int
	err := db.QueryRow(query, userID).Scan(&count)
	return count, err
}

// ErrInstanceLimitReached is returned when a user has reached their instance creation limit.
var ErrInstanceLimitReached = errors.New("instance limit reached")

// CreateInstanceAtomic inserts an instance and assigns it to a user within a single
// transaction protected by a PostgreSQL advisory lock, preventing TOCTOU races on
// the per-user instance count limit.
func CreateInstanceAtomic(instance *Instance, userID int64, maxInstances int) error {
	db := database.AppDB
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Acquire a per-user advisory lock for the duration of this transaction.
	// Concurrent requests for the same user serialize here.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", userID); err != nil {
		return err
	}

	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_instances WHERE user_id = $1", userID).Scan(&count); err != nil {
		return err
	}
	if count >= maxInstances {
		return ErrInstanceLimitReached
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO instances (instance_id, status, is_connected, created_at, session_data, circle, used, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		instance.InstanceID, instance.Status, instance.IsConnected, instance.CreatedAt,
		instance.SessionData, instance.Circle, true, instance.CreatedBy,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_instances (user_id, instance_id, permission_level)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, instance_id) DO UPDATE SET permission_level = $3`,
		userID, instance.InstanceID, "access",
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

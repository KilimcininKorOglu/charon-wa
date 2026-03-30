package model

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"hermeswa/database"
)

type APIKey struct {
	ID          int        `json:"id"`
	UserID      int        `json:"user_id"`
	KeyPrefix   string     `json:"key_prefix"`
	Name        string     `json:"name"`
	Application string     `json:"application,omitempty"`
	Enabled     bool       `json:"enabled"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	// Joined fields (from users table)
	Username string `json:"username,omitempty"`
	Role     string `json:"role,omitempty"`
}

// generateRawKey creates a random API key: hwa_ + 32 hex chars
func generateRawKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "hwa_" + hex.EncodeToString(b), nil
}

// hashKey returns SHA-256 hex digest of the raw key
func hashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}

// CreateAPIKey generates a new API key, stores its hash, and returns the raw key (shown once)
func CreateAPIKey(ctx context.Context, userID int, name string, application string) (string, *APIKey, error) {
	rawKey, err := generateRawKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate key: %w", err)
	}

	keyHash := hashKey(rawKey)
	keyPrefix := rawKey[:8] // "hwa_xxxx"

	var appVal sql.NullString
	if application != "" {
		appVal = sql.NullString{String: application, Valid: true}
	}

	var key APIKey
	err = database.AppDB.QueryRowContext(ctx,
		`INSERT INTO api_keys (user_id, key_hash, key_prefix, name, application)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, key_prefix, name, COALESCE(application, ''), enabled, created_at`,
		userID, keyHash, keyPrefix, name, appVal,
	).Scan(&key.ID, &key.UserID, &key.KeyPrefix, &key.Name, &key.Application, &key.Enabled, &key.CreatedAt)
	if err != nil {
		return "", nil, fmt.Errorf("failed to insert api key: %w", err)
	}

	return rawKey, &key, nil
}

// ValidateAPIKey checks a raw key against stored hashes and returns the key with user info
func ValidateAPIKey(ctx context.Context, rawKey string) (*APIKey, error) {
	keyHash := hashKey(rawKey)

	var key APIKey
	var lastUsed sql.NullTime
	var app sql.NullString

	err := database.AppDB.QueryRowContext(ctx,
		`SELECT k.id, k.user_id, k.key_prefix, k.name, k.application, k.enabled, k.last_used_at, k.created_at,
		        u.username, u.role
		 FROM api_keys k
		 JOIN users u ON u.id = k.user_id
		 WHERE k.key_hash = $1 AND k.enabled = true AND u.is_active = true`,
		keyHash,
	).Scan(&key.ID, &key.UserID, &key.KeyPrefix, &key.Name, &app, &key.Enabled, &lastUsed, &key.CreatedAt,
		&key.Username, &key.Role)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid or disabled api key")
		}
		return nil, fmt.Errorf("failed to validate api key: %w", err)
	}

	if app.Valid {
		key.Application = app.String
	}
	if lastUsed.Valid {
		key.LastUsedAt = &lastUsed.Time
	}

	return &key, nil
}

// UpdateAPIKeyLastUsed updates the last_used_at timestamp
func UpdateAPIKeyLastUsed(ctx context.Context, keyID int) {
	_, _ = database.AppDB.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, keyID)
}

// ListAPIKeys returns all API keys for a user (without hashes)
func ListAPIKeys(ctx context.Context, userID int) ([]APIKey, error) {
	rows, err := database.AppDB.QueryContext(ctx,
		`SELECT id, user_id, key_prefix, name, COALESCE(application, ''), enabled, last_used_at, created_at
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var lastUsed sql.NullTime
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyPrefix, &k.Name, &k.Application, &k.Enabled, &lastUsed, &k.CreatedAt); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			k.LastUsedAt = &lastUsed.Time
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// DeleteAPIKey removes an API key (user can only delete own keys)
func DeleteAPIKey(ctx context.Context, keyID int, userID int) error {
	res, err := database.AppDB.ExecContext(ctx,
		`DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("api key not found")
	}
	return nil
}

package model

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"charon/database"
)

// AuthSession represents a cookie-based user session stored in the database
type AuthSession struct {
	ID           int64
	SessionID    string // SHA-256 hash stored in DB
	UserID       int64
	Username     string
	Role         string
	IPAddress    sql.NullString
	UserAgent    sql.NullString
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastActiveAt time.Time
}

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
	ErrUserInactive    = errors.New("user inactive")
)

func hashSessionToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash)
}

// CreateAuthSession creates a new session and returns the raw (unhashed) token
func CreateAuthSession(userID int64, username, role, ipAddress, userAgent string, expiry time.Duration) (string, error) {
	db := database.AppDB

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	rawToken := hex.EncodeToString(b)
	hashedToken := hashSessionToken(rawToken)

	_, err := db.Exec(`
		INSERT INTO sessions (session_id, user_id, username, role, ip_address, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		hashedToken, userID, username, role, ipAddress, userAgent, time.Now().Add(expiry),
	)
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

// GetAuthSessionByToken validates a raw token and returns the session
func GetAuthSessionByToken(rawToken string) (*AuthSession, error) {
	db := database.AppDB
	hashedToken := hashSessionToken(rawToken)

	s := &AuthSession{}
	var isActive bool
	var currentRole string
	err := db.QueryRow(`
		SELECT s.id, s.session_id, s.user_id, s.username, s.role, s.ip_address, s.user_agent,
		       s.created_at, s.expires_at, s.last_active_at,
		       u.is_active, u.role
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.session_id = $1`,
		hashedToken,
	).Scan(&s.ID, &s.SessionID, &s.UserID, &s.Username, &s.Role,
		&s.IPAddress, &s.UserAgent, &s.CreatedAt, &s.ExpiresAt, &s.LastActiveAt,
		&isActive, &currentRole)

	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	if time.Now().After(s.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	if !isActive {
		// Destroy all sessions of the disabled user — instant revocation
		_, _ = db.Exec(`DELETE FROM sessions WHERE user_id = $1`, s.UserID)
		return nil, ErrUserInactive
	}

	// Keep the session's cached role in sync with the current role on the user row
	s.Role = currentRole

	return s, nil
}

// TouchAuthSession updates last_active_at and extends expiry (sliding window)
func TouchAuthSession(hashedSessionID string, expiry time.Duration) error {
	db := database.AppDB
	_, err := db.Exec(`
		UPDATE sessions SET last_active_at = NOW(), expires_at = $1
		WHERE session_id = $2`,
		time.Now().Add(expiry), hashedSessionID,
	)
	return err
}

// DeleteAuthSession removes a session by raw token
func DeleteAuthSession(rawToken string) error {
	db := database.AppDB
	hashedToken := hashSessionToken(rawToken)
	_, err := db.Exec(`DELETE FROM sessions WHERE session_id = $1`, hashedToken)
	return err
}

// DeleteAllUserAuthSessions removes all sessions for a user (instant revocation)
func DeleteAllUserAuthSessions(userID int64) error {
	db := database.AppDB
	_, err := db.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

// CleanupExpiredAuthSessions removes expired sessions
func CleanupExpiredAuthSessions() (int64, error) {
	db := database.AppDB
	result, err := db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

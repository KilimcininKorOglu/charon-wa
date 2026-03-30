// internal/model/token_blacklist.go
package model

import (
	"strconv"
	"time"

	"hermeswa/database"
)

// TokenBlacklist represents a blacklisted access token
type TokenBlacklist struct {
	ID        int64
	Token     string
	UserID    int64
	Reason    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// BlacklistToken adds a token to the blacklist
func BlacklistToken(token string, userID int64, reason string, expiresAt time.Time) error {
	db := database.AppDB

	query := `
		INSERT INTO token_blacklist (token, user_id, reason, expires_at)
		VALUES ($1, $2, $3, $4)
	`

	_, err := db.Exec(query, token, userID, reason, expiresAt)
	return err
}

// IsTokenBlacklisted checks if a token is blacklisted
func IsTokenBlacklisted(token string) (bool, error) {
	db := database.AppDB

	query := `
		SELECT COUNT(*) 
		FROM token_blacklist 
		WHERE token = $1 AND expires_at > NOW()
	`

	var count int
	err := db.QueryRow(query, token).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// BlacklistAllUserTokens inserts a marker that invalidates all tokens for a user
func BlacklistAllUserTokens(userID int64, reason string) error {
	db := database.AppDB

	query := `
		INSERT INTO token_blacklist (token, user_id, reason, expires_at)
		VALUES ($1, $2, $3, NOW() + INTERVAL '24 hours')
	`

	markerToken := "USER_" + strconv.FormatInt(userID, 10) + "_INVALIDATE_ALL"
	_, err := db.Exec(query, markerToken, userID, reason)
	return err
}

// IsUserBlacklisted checks if all tokens for a user have been invalidated
func IsUserBlacklisted(userID int64) (bool, error) {
	db := database.AppDB

	query := `
		SELECT COUNT(*) FROM token_blacklist
		WHERE user_id = $1 AND token LIKE 'USER_%_INVALIDATE_ALL' AND expires_at > NOW()
	`

	var count int
	err := db.QueryRow(query, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CleanupExpiredBlacklistedTokens removes expired tokens from blacklist
func CleanupExpiredBlacklistedTokens() (int64, error) {
	db := database.AppDB

	query := `DELETE FROM token_blacklist WHERE expires_at < NOW()`

	result, err := db.Exec(query)
	if err != nil {
		return 0, err
	}

	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsDeleted, nil
}

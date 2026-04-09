package model

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"charon/database"
)

var (
	ErrTicketNotFound = errors.New("ticket not found")
	ErrTicketExpired  = errors.New("ticket expired")
	ErrTicketUsed     = errors.New("ticket already used")
)

// CreateWSTicket generates a one-time ticket for WebSocket authentication
func CreateWSTicket(userID int64, role string) (string, error) {
	db := database.AppDB
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(b)
	expiresAt := time.Now().Add(30 * time.Second)

	_, err := db.Exec(
		`INSERT INTO ws_tickets (ticket, user_id, role, expires_at) VALUES ($1, $2, $3, $4)`,
		ticket, userID, role, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return ticket, nil
}

// ConsumeWSTicket atomically validates and marks a ticket as used
func ConsumeWSTicket(ticket string) (int64, string, error) {
	db := database.AppDB
	tx, err := db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	var userID int64
	var role string
	var expiresAt time.Time
	var used bool

	err = tx.QueryRow(
		`SELECT user_id, role, expires_at, used FROM ws_tickets WHERE ticket = $1 FOR UPDATE`,
		ticket,
	).Scan(&userID, &role, &expiresAt, &used)

	if err == sql.ErrNoRows {
		return 0, "", ErrTicketNotFound
	}
	if err != nil {
		return 0, "", err
	}
	if used {
		return 0, "", ErrTicketUsed
	}
	if time.Now().After(expiresAt) {
		return 0, "", ErrTicketExpired
	}

	_, err = tx.Exec(`UPDATE ws_tickets SET used = true WHERE ticket = $1`, ticket)
	if err != nil {
		return 0, "", err
	}

	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return userID, role, nil
}

// CleanupExpiredTickets removes old tickets
func CleanupExpiredTickets() (int64, error) {
	db := database.AppDB
	result, err := db.Exec(`DELETE FROM ws_tickets WHERE expires_at < NOW() - INTERVAL '1 hour'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

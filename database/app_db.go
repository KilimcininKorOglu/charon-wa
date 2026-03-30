package database

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

var AppDB *sql.DB
var OutboxDB *sql.DB

// Initialize connection to custom database (not whatsmeow)
func InitAppDB(appDbURL string) {
	db, err := sql.Open("postgres", appDbURL)
	if err != nil {
		log.Fatal("Failed to connect app DB:", err)
	}
	AppDB = db
	err = AppDB.Ping()
	if err != nil {
		log.Fatal("Failed to ping app DB:", err)
	}
	log.Println("App DB (custom) connected successfully")
}

// InitOutboxDB initializes connection to outbox database (can be same or different from AppDB)
func InitOutboxDB(outboxURL string) {
	if outboxURL == "" {
		log.Println("OUTBOX_DATABASE_URL not set, falling back to AppDB for outbox features")
		OutboxDB = AppDB
		return
	}

	db, err := sql.Open("postgres", outboxURL)
	if err != nil {
		log.Printf("⚠️ Warning: Failed to open Outbox DB: %v", err)
		OutboxDB = AppDB
		return
	}

	if err := db.Ping(); err != nil {
		log.Printf("⚠️ Warning: Failed to ping Outbox DB: %v. Falling back to AppDB.", err)
		OutboxDB = AppDB
		return
	}

	OutboxDB = db
	log.Println("Outbox DB connected successfully")
}

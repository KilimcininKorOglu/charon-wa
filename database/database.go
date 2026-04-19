package database

import (
	"context"
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

var (
	Container   *sqlstore.Container
	WhatsmeowDB *sql.DB
)

func InitWhatsmeow(dbURL string) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Failed to connect database:", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)
	WhatsmeowDB = db

	// Create whatsmeow container
	Container = sqlstore.NewWithDB(db, "postgres", nil)

	// Upgrade with context
	err = Container.Upgrade(context.Background())
	if err != nil {
		log.Fatal("Failed to upgrade database:", err)
	}

	log.Println("Database connected successfully")
}

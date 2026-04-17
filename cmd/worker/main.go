package main

import (
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var (
	ConfigDB *sql.DB
	OutboxDB *sql.DB
)

func initDB() {
	// 1. Initialise ConfigDB (always APP_DATABASE_URL)
	appDBURL := os.Getenv("APP_DATABASE_URL")
	if appDBURL == "" {
		log.Fatal("APP_DATABASE_URL is not set")
	}
	ConfigDB = connectDB(appDBURL, "Config")

	// 2. Initialise OutboxDB (OUTBOX_DATABASE_URL or fallback)
	outboxDBURL := os.Getenv("OUTBOX_DATABASE_URL")
	if outboxDBURL == "" {
		log.Println("OUTBOX_DATABASE_URL not set, falling back to APP_DATABASE_URL for outbox")
		OutboxDB = ConfigDB
	} else {
		OutboxDB = connectDB(outboxDBURL, "Outbox")
	}
}

func connectDB(dbURL, label string) *sql.DB {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to open %s database: %v", label, err)
	}

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping %s database: %v", label, err)
	}

	log.Printf("Successfully connected to %s database", label)
	return db
}

func main() {
	// 1. Load configuration
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("../../.env")
	}

	// 2. Initialize database
	initDB()
	defer ConfigDB.Close()
	if OutboxDB != ConfigDB {
		defer OutboxDB.Close()
	}

	// 3. Worker Configuration
	apiBaseURL := os.Getenv("OUTBOX_API_BASEURL")
	if apiBaseURL == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "2121"
		}
		apiBaseURL = "http://localhost:" + port
	}

	apiKey := os.Getenv("WORKER_API_KEY")
	if apiKey == "" {
		log.Fatal("WORKER_API_KEY is not set (create an admin API key via the web UI)")
	}

	// 4. Initialize API Client
	client := NewCharonClient(apiBaseURL, apiKey)

	// 5. Start Worker Manager
	manager := NewWorkerManager(client)
	manager.Start()

	// 6. Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down worker...")
	manager.Stop()
	log.Println("Worker shutdown complete.")
}

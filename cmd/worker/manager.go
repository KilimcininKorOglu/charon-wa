package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type WorkerManager struct {
	client  *CharonClient
	workers map[int]*WorkerInstance
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewWorkerManager(client *CharonClient) *WorkerManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerManager{
		client:  client,
		workers: make(map[int]*WorkerInstance),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (m *WorkerManager) Start() {
	log.Println("Worker Blast Outbox Manager started. Polling for configurations...")

	// Sweep any pre-existing orphaned `status=3` rows on startup so crash-left
	// messages from the last run are requeued immediately.
	if n, err := ReapStaleClaims(m.ctx, 10*time.Minute); err != nil {
		log.Printf("Startup reaper error: %v", err)
	} else if n > 0 {
		log.Printf("Startup reaper requeued %d stale outbox rows", n)
	}

	// Initial load
	refreshPhoneRegion(m.ctx)
	m.reloadConfigs()

	// Periodic reload every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	reaper := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				refreshPhoneRegion(m.ctx)
				m.reloadConfigs()
			case <-reaper.C:
				if n, err := ReapStaleClaims(m.ctx, 10*time.Minute); err != nil {
					log.Printf("Reaper error: %v", err)
				} else if n > 0 {
					log.Printf("Reaper requeued %d stale outbox rows", n)
				}
			case <-m.ctx.Done():
				ticker.Stop()
				reaper.Stop()
				return
			}
		}
	}()
}

func (m *WorkerManager) reloadConfigs() {
	log.Println("Reloading blast outbox configurations from database...")

	configs, err := FetchWorkerConfigs(m.ctx)
	if err != nil {
		msg := fmt.Sprintf("Error fetching worker configs: %v", err)
		log.Print(msg)
		LogWorkerEvent(0, "SYSTEM", "ERROR", msg)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Keep track of configs we found to identify ones that need to be removed
	activeConfigIDs := make(map[int]bool)

	for _, config := range configs {
		activeConfigIDs[config.ID] = true
		m.applyConfig(config)
	}

	// Stop workers whose configs are no longer in DB or are disabled
	for id, worker := range m.workers {
		if !activeConfigIDs[id] {
			log.Printf("Worker ID %d (%s) is no longer active. Stopping...", id, worker.config.WorkerName)
			worker.Stop()
			delete(m.workers, id)
		}
	}
}

// needsRestart reports whether a running worker's settings no longer match the
// config just read from the database.
func needsRestart(running, latest WorkerConfig) bool {
	return running.IntervalSeconds != latest.IntervalSeconds ||
		running.IntervalMaxSeconds != latest.IntervalMaxSeconds ||
		running.Circle != latest.Circle ||
		running.Application != latest.Application ||
		running.MessageType != latest.MessageType
}

// applyConfig starts a worker for a new config, or restarts the running one
// when its settings changed. The caller must hold m.mu.
func (m *WorkerManager) applyConfig(config WorkerConfig) {
	existingWorker, exists := m.workers[config.ID]

	switch {
	case !exists:
		log.Printf("Starting new worker ID %d: %s (Application: %s, Circle: %s, Interval: %ds)",
			config.ID, config.WorkerName, config.Application, config.Circle, config.IntervalSeconds)
	case needsRestart(existingWorker.config, config):
		log.Printf("Config changed for worker ID %d (%s). Restarting...", config.ID, config.WorkerName)
		existingWorker.Stop()
	default:
		return
	}

	newWorker := NewWorkerInstance(config, m.client)
	m.workers[config.ID] = newWorker
	go newWorker.Start()
}

func (m *WorkerManager) Stop() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, worker := range m.workers {
		worker.Stop()
	}
	log.Println("Worker Manager stopped.")
}

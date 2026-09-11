package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"charon/internal/helper"
)

// workerWebhookClient is shared across all webhook deliveries from the worker
// so that keep-alive and the SSRF-safe transport aren't rebuilt per request.
var workerWebhookClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext: helper.SSRFSafeDialContext,
	},
}

type WorkerInstance struct {
	config  WorkerConfig
	client  *CharonClient
	ctx     context.Context
	cancel  context.CancelFunc
	counter int // Round-robin counter
	wg      sync.WaitGroup
}

func NewWorkerInstance(config WorkerConfig, client *CharonClient) *WorkerInstance {
	// WorkerName is operator-supplied and prefixes every log line this worker
	// writes. Sanitize it once here so no later call site can forge a log entry.
	config.WorkerName = helper.SanitizeLogValue(config.WorkerName)

	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerInstance{
		config: config,
		client: client,
		ctx:    ctx,
		cancel: cancel,
	}
}

// logf writes one log line prefixed with this worker's name.
//
// Every value that reaches it is already safe: NewWorkerInstance sanitizes
// WorkerName once, and each caller passes any other operator-supplied or
// remote value through helper.SanitizeLogValue. gosec cannot follow a custom
// sanitizer, so the G706 justification lives here instead of at every call site.
func (w *WorkerInstance) logf(format string, args ...any) {
	// #nosec G706
	log.Printf("[%s] "+format, append([]any{w.config.WorkerName}, args...)...)
}

func (w *WorkerInstance) Start() {
	w.wg.Add(1)
	defer w.wg.Done()

	w.logf("Worker started")
	LogWorkerEvent(w.config.ID, w.config.WorkerName, "INFO", "Worker started")

	for {
		select {
		case <-w.ctx.Done():
			w.logf("Worker shutting down...")
			return
		default:
			w.runCycle()

			// Calculate sleep duration
			sleepSeconds := w.config.IntervalSeconds
			if w.config.IntervalMaxSeconds > w.config.IntervalSeconds {
				rangeSec := w.config.IntervalMaxSeconds - w.config.IntervalSeconds + 1
				// Poll interval jitter, not a secret.
				// #nosec G404
				sleepSeconds = w.config.IntervalSeconds + rand.Intn(rangeSec)
			}

			// Interruptible sleep
			select {
			case <-w.ctx.Done():
				w.logf("Worker shutting down during sleep...")
				return
			case <-time.After(time.Duration(sleepSeconds) * time.Second):
				// Just continue to next cycle
			}
		}
	}
}

func (w *WorkerInstance) Stop() {
	LogWorkerEvent(w.config.ID, w.config.WorkerName, "INFO", "Worker stopping")
	w.cancel()
	w.wg.Wait()
}

func (w *WorkerInstance) runCycle() {
	// 1. Build Application Filter
	// Supports: "App1" (Single), "App1, App2, App3" (Multi), "*" or "" (Wildcard)
	var applications []string
	if w.config.Application != "*" && w.config.Application != "" {
		for _, a := range strings.Split(w.config.Application, ",") {
			if trimmed := strings.TrimSpace(a); trimmed != "" {
				applications = append(applications, trimmed)
			}
		}
	}

	// 2. Claim a pending message scoped to this worker's owner (tenant isolation).
	msg, err := ClaimPendingOutbox(w.ctx, applications, w.config.UserID)

	if err != nil {
		if err != sql.ErrNoRows {
			w.logf("Error claiming outbox: %v", err)
		}
		return
	}

	w.logf("Processing message ID: %d to %s", msg.ID, helper.SanitizeLogValue(msg.Destination))

	// 2. Validate and Normalize Destination
	destination := msg.Destination
	if w.config.MessageType == "group" {
		// Group ID normalization: append @g.us if missing
		if !strings.Contains(destination, "@") {
			destination = destination + "@g.us"
			w.logf("Normalized Group ID: %s", helper.SanitizeLogValue(destination))
		}
	} else {
		// Direct Message normalization
		cleaned := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, destination)

		if strings.HasPrefix(cleaned, "0") {
			cleaned = "62" + cleaned[1:]
		}

		if !strings.HasPrefix(cleaned, "62") || len(cleaned) < 10 {
			w.logf("Invalid phone number format: %s", helper.SanitizeLogValue(destination))
			if err := UpdateOutboxFailed(w.ctx, msg.ID, "Invalid phone number format"); err != nil {
				w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msg.ID, err)
			}
			return
		}
		destination = cleaned
	}

	// 3. Get Instances for this Circle
	instances, err := w.client.GetInstances(w.ctx, w.config.Circle)
	if err != nil {
		errMsg := fmt.Sprintf("Error fetching instances: %v", err)
		w.logf("%s", helper.SanitizeLogValue(errMsg))
		LogWorkerEvent(w.config.ID, w.config.WorkerName, "ERROR", errMsg)
		if err := UpdateOutboxFailed(context.Background(), msg.ID, errMsg); err != nil {
			w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msg.ID, err)
		}
		return
	}

	if len(instances) == 0 {
		errMsg := fmt.Sprintf("No used instances found in circle: %s", w.config.Circle)
		w.logf("%s", helper.SanitizeLogValue(errMsg))
		LogWorkerEvent(w.config.ID, w.config.WorkerName, "WARN", errMsg)
		if err := UpdateOutboxFailed(context.Background(), msg.ID, errMsg); err != nil {
			w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msg.ID, err)
		}
		return
	}

	// 4. Select Instance (Round-Robin)
	selectedInstance := instances[w.counter%len(instances)]
	w.counter++

	// 5. Send Message
	var success bool
	var apiMsg string

	if w.config.AllowMedia && msg.File.Valid && msg.File.String != "" {
		// Media Message (File with Caption from Messages)
		if w.config.MessageType == "group" {
			success, apiMsg, err = w.client.SendGroupMediaURL(w.ctx, selectedInstance.InstanceID, destination, msg.File.String, msg.Messages)
		} else {
			success, apiMsg, err = w.client.SendMediaURL(w.ctx, selectedInstance.InstanceID, destination, msg.File.String, msg.Messages)
		}
	} else {
		// Text Message
		if w.config.MessageType == "group" {
			success, apiMsg, err = w.client.SendGroupMessage(w.ctx, selectedInstance.InstanceID, destination, msg.Messages)
		} else {
			success, apiMsg, err = w.client.SendMessage(w.ctx, selectedInstance.InstanceID, destination, msg.Messages)
		}
	}

	if err != nil {
		errMsg := fmt.Sprintf("Error calling API (Instance %s): %v", selectedInstance.InstanceID, err)
		w.logf("%s", helper.SanitizeLogValue(errMsg))
		LogWorkerEvent(w.config.ID, w.config.WorkerName, "ERROR", errMsg)
		if err := UpdateOutboxFailed(context.Background(), msg.ID, errMsg); err != nil {
			w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msg.ID, err)
		}
		return
	}

	if success {
		w.logf("Success! Sent ID %d via instance %s (%s)", msg.ID, selectedInstance.InstanceID, selectedInstance.PhoneNumber)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := UpdateOutboxSuccess(dbCtx, msg.ID, selectedInstance.PhoneNumber); err != nil {
			w.logf("CRITICAL: Failed to update status to success for ID %d: %v", msg.ID, err)
		}

		dbCancel()

		// Trigger Webhook
		go w.sendWebhook(msg, 1, "success", selectedInstance.PhoneNumber, "")

		// Optional: delay after success to prevent mass-ban
		// Send pacing jitter, not a secret.
		// #nosec G404
		time.Sleep(time.Duration(rand.Intn(2)+1) * time.Second)
	} else {
		w.logf("Failed sending ID %d: %s", msg.ID, helper.SanitizeLogValue(apiMsg))
		dbCtx2, dbCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		if err := UpdateOutboxFailed(dbCtx2, msg.ID, apiMsg); err != nil {
			w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msg.ID, err)
		}
		dbCancel2()

		// Log significant failures (like 401 or specific API errors)
		if strings.Contains(strings.ToLower(apiMsg), "unauthorized") || strings.Contains(strings.ToLower(apiMsg), "forbidden") {
			LogWorkerEvent(w.config.ID, w.config.WorkerName, "ERROR", fmt.Sprintf("API Authorization Error: %s", apiMsg))
		}

		// Trigger Webhook
		go w.sendWebhook(msg, 2, "failed", "", apiMsg)
	}
}

func (w *WorkerInstance) sendWebhook(msg *OutboxMessage, status int, statusText string, fromNumber string, errorMsg string) {
	webhookURL := strings.TrimSpace(w.config.WebhookURL.String)
	if webhookURL == "" {
		return
	}

	payload := map[string]interface{}{
		"event":     "outbox.processed",
		"timestamp": time.Now().UTC(),
		"data": map[string]interface{}{
			"id_outbox":   msg.ID,
			"status":      status,
			"status_text": statusText,
			"destination": msg.Destination,
			"from_number": fromNumber,
			"application": msg.Application,
			"table_id":    msg.TableID.String,
			"error_msg":   errorMsg,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		w.logf("Webhook marshal error: %v", err)
		return
	}

	w.logf("Sending webhook to: %s", helper.SanitizeLogValue(webhookURL))

	// The URL is operator-supplied and guarded twice: handler.CreateWorkerConfig
	// and handler.UpdateWorkerConfig run helper.ValidateExternalURL before the row
	// is written, and workerWebhookClient dials through helper.SSRFSafeDialContext,
	// which rejects a private or reserved IP at connect time even after a rebind.
	// #nosec G704
	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		w.logf("Webhook request error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Add timestamped HMAC signature if secret is provided.
	// Receivers must reject timestamps older than their replay window (recommend 5 min).
	secret := w.config.WebhookSecret.String
	if secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp))
		mac.Write([]byte("."))
		mac.Write(body)
		signature := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Charon-Timestamp", timestamp)
		req.Header.Set("X-Charon-Signature", signature)
	}

	// #nosec G704
	resp, err := workerWebhookClient.Do(req)
	if err != nil {
		w.logf("Webhook send error: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		w.logf("Webhook returned error status %d: %s", resp.StatusCode, helper.SanitizeLogValue(string(respBody)))
	} else {
		w.logf("Webhook sent successfully to %s", helper.SanitizeLogValue(webhookURL))
	}
}

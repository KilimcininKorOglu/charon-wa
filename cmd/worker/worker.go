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

// applicationFilter turns the worker's Application setting into the list of
// application names to claim for. Supports "App1" (single), "App1, App2"
// (multi), and "*" or "" (wildcard, which claims everything).
func (w *WorkerInstance) applicationFilter() []string {
	if w.config.Application == "*" || w.config.Application == "" {
		return nil
	}

	var applications []string
	for a := range strings.SplitSeq(w.config.Application, ",") {
		if trimmed := strings.TrimSpace(a); trimmed != "" {
			applications = append(applications, trimmed)
		}
	}
	return applications
}

// failOutbox marks a message failed, logs the reason, and records a worker event.
// It always runs on a background context so a cancelled worker still persists
// the outcome.
func (w *WorkerInstance) failOutbox(msgID int64, level, errMsg string) {
	w.logf("%s", helper.SanitizeLogValue(errMsg))
	LogWorkerEvent(w.config.ID, w.config.WorkerName, level, errMsg)
	if err := UpdateOutboxFailed(context.Background(), msgID, errMsg); err != nil {
		w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msgID, err)
	}
}

// normalizeGroupDestination appends the group suffix when it is missing.
func (w *WorkerInstance) normalizeGroupDestination(destination string) string {
	if strings.Contains(destination, "@") {
		return destination
	}
	normalized := destination + "@g.us"
	w.logf("Normalized Group ID: %s", helper.SanitizeLogValue(normalized))
	return normalized
}

// resolveDestination normalizes the message destination for the worker's
// message type. It marks the message failed and returns false when the
// destination is unusable.
func (w *WorkerInstance) resolveDestination(msg *OutboxMessage) (string, bool) {
	if w.config.MessageType == "group" {
		return w.normalizeGroupDestination(msg.Destination), true
	}

	cleaned, err := helper.NormalizePhone(msg.Destination)
	if err != nil {
		w.logf("Invalid phone number format: %s (%v)", helper.SanitizeLogValue(msg.Destination), err)
		if err := UpdateOutboxFailed(w.ctx, msg.ID, "Invalid phone number format"); err != nil {
			w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msg.ID, err)
		}
		return "", false
	}
	return cleaned, true
}

// pickInstance returns the next instance in the circle by round-robin. It marks
// the message failed and returns false when no instance is available.
func (w *WorkerInstance) pickInstance(msgID int64) (InstanceInfo, bool) {
	instances, err := w.client.GetInstances(w.ctx, w.config.Circle)
	if err != nil {
		w.failOutbox(msgID, "ERROR", fmt.Sprintf("Error fetching instances: %v", err))
		return InstanceInfo{}, false
	}
	if len(instances) == 0 {
		w.failOutbox(msgID, "WARN", fmt.Sprintf("No used instances found in circle: %s", w.config.Circle))
		return InstanceInfo{}, false
	}

	selected := instances[w.counter%len(instances)]
	w.counter++
	return selected, true
}

// dispatch sends one message through the API, choosing the media or text
// endpoint and the group or direct variant from the worker configuration.
func (w *WorkerInstance) dispatch(instanceID, destination string, msg *OutboxMessage) (bool, string, error) {
	isGroup := w.config.MessageType == "group"

	if w.config.AllowMedia && msg.File.Valid && msg.File.String != "" {
		// Media Message (File with Caption from Messages)
		if isGroup {
			return w.client.SendGroupMediaURL(w.ctx, instanceID, destination, msg.File.String, msg.Messages)
		}
		return w.client.SendMediaURL(w.ctx, instanceID, destination, msg.File.String, msg.Messages)
	}

	if isGroup {
		return w.client.SendGroupMessage(w.ctx, instanceID, destination, msg.Messages)
	}
	return w.client.SendMessage(w.ctx, instanceID, destination, msg.Messages)
}

// recordSuccess persists the delivery, fires the webhook, and paces the next
// send so a burst does not look like spam.
func (w *WorkerInstance) recordSuccess(msg *OutboxMessage, instance InstanceInfo) {
	w.logf("Success! Sent ID %d via instance %s (%s)", msg.ID, instance.InstanceID, instance.PhoneNumber)

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := UpdateOutboxSuccess(dbCtx, msg.ID, instance.PhoneNumber); err != nil {
		w.logf("CRITICAL: Failed to update status to success for ID %d: %v", msg.ID, err)
	}
	dbCancel()

	go w.sendWebhook(msg, 1, "success", instance.PhoneNumber, "")

	// Optional: delay after success to prevent mass-ban
	// Send pacing jitter, not a secret.
	// #nosec G404
	time.Sleep(time.Duration(rand.Intn(2)+1) * time.Second)
}

// recordAPIFailure persists an API-reported failure and fires the webhook.
func (w *WorkerInstance) recordAPIFailure(msg *OutboxMessage, apiMsg string) {
	w.logf("Failed sending ID %d: %s", msg.ID, helper.SanitizeLogValue(apiMsg))

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := UpdateOutboxFailed(dbCtx, msg.ID, apiMsg); err != nil {
		w.logf("CRITICAL: Failed to update status to failed for ID %d: %v", msg.ID, err)
	}
	dbCancel()

	// Log significant failures (like 401 or specific API errors)
	lowered := strings.ToLower(apiMsg)
	if strings.Contains(lowered, "unauthorized") || strings.Contains(lowered, "forbidden") {
		LogWorkerEvent(w.config.ID, w.config.WorkerName, "ERROR", fmt.Sprintf("API Authorization Error: %s", apiMsg))
	}

	go w.sendWebhook(msg, 2, "failed", "", apiMsg)
}

// runCycle claims one pending outbox message and delivers it. Every exit path
// leaves the message in a terminal state, never claimed-and-forgotten.
func (w *WorkerInstance) runCycle() {
	msg, err := ClaimPendingOutbox(w.ctx, w.applicationFilter(), w.config.UserID)
	if err != nil {
		if err != sql.ErrNoRows {
			w.logf("Error claiming outbox: %v", err)
		}
		return
	}

	w.logf("Processing message ID: %d to %s", msg.ID, helper.SanitizeLogValue(msg.Destination))

	destination, ok := w.resolveDestination(msg)
	if !ok {
		return
	}

	selectedInstance, ok := w.pickInstance(msg.ID)
	if !ok {
		return
	}

	success, apiMsg, err := w.dispatch(selectedInstance.InstanceID, destination, msg)
	if err != nil {
		w.failOutbox(msg.ID, "ERROR", fmt.Sprintf("Error calling API (Instance %s): %v", selectedInstance.InstanceID, err))
		return
	}

	if success {
		w.recordSuccess(msg, selectedInstance)
		return
	}
	w.recordAPIFailure(msg, apiMsg)
}

func (w *WorkerInstance) sendWebhook(msg *OutboxMessage, status int, statusText string, fromNumber string, errorMsg string) {
	webhookURL := strings.TrimSpace(w.config.WebhookURL.String)
	if webhookURL == "" {
		return
	}

	payload := map[string]any{
		"event":     "outbox.processed",
		"timestamp": time.Now().UTC(),
		"data": map[string]any{
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

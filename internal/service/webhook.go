package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"charon/internal/helper"
	"charon/internal/model"
)

// Webhook config struct with TTL
type WebhookConfig struct {
	URL       string
	Secret    string
	ExpiresAt time.Time // Expiry time for cache entry
}

// Cache for webhook config (avoid N+1 query)
var (
	webhookCache      = make(map[string]*WebhookConfig)
	webhookCacheMutex sync.RWMutex
	webhookCacheTTL   = 5 * time.Minute // Cache valid for 5 minutes
)

type WebhookPayload struct {
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// Get webhook config with caching + TTL
func GetWebhookConfig(instanceID string) (*WebhookConfig, error) {
	// Check cache first
	webhookCacheMutex.RLock()
	config, exists := webhookCache[instanceID]
	webhookCacheMutex.RUnlock()

	// Check if cache is still valid (not expired)
	if exists && config != nil && time.Now().Before(config.ExpiresAt) {
		return config, nil
	}

	// Cache miss or expired - load from DB
	inst, err := model.GetInstanceByInstanceID(instanceID)
	if err != nil {
		return nil, err
	}

	// Create config object with expiry time
	config = &WebhookConfig{
		URL:       inst.WebhookURL.String,
		Secret:    inst.WebhookSecret.String,
		ExpiresAt: time.Now().Add(webhookCacheTTL), // Set expiry
	}

	// Save to cache
	webhookCacheMutex.Lock()
	webhookCache[instanceID] = config
	webhookCacheMutex.Unlock()

	log.Printf("✅ Webhook config cached for instance: %s (expires in %v)", instanceID, webhookCacheTTL)
	return config, nil
}

// Invalidate cache (called when webhook config is updated)
func InvalidateWebhookCache(instanceID string) {
	webhookCacheMutex.Lock()
	delete(webhookCache, instanceID)
	webhookCacheMutex.Unlock()
	log.Printf("🗑️ Webhook cache invalidated for instance: %s", instanceID)
}

// webhookCacheSweepInterval controls how often the expired-entry sweeper runs.
var webhookCacheSweepInterval = 10 * time.Minute

// sweepExpiredWebhookCache walks the cache once and evicts expired entries.
func sweepExpiredWebhookCache() {
	now := time.Now()
	webhookCacheMutex.Lock()
	defer webhookCacheMutex.Unlock()
	for id, cfg := range webhookCache {
		if cfg == nil || now.After(cfg.ExpiresAt) {
			delete(webhookCache, id)
		}
	}
}

// StartWebhookCacheSweeper launches a background goroutine that periodically
// removes expired webhook cache entries. Safe to call once at startup.
func StartWebhookCacheSweeper() {
	go func() {
		ticker := time.NewTicker(webhookCacheSweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			sweepExpiredWebhookCache()
		}
	}()
	log.Printf("🧹 Webhook cache sweeper started (interval: %v)", webhookCacheSweepInterval)
}

// webhookRetryBackoffs is the delay sequence between retry attempts
// (initial attempt + 3 retries = 4 total tries).
var webhookRetryBackoffs = []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second}

// webhookHTTPClient is reused across webhook deliveries so that keep-alive
// connections and the SSRF-safe transport are not re-created per call.
var webhookHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DialContext: helper.SSRFSafeDialContext,
	},
}

// Refactored function - now uses cache
func SendIncomingMessageWebhook(instanceID string, data map[string]any) {
	// Get webhook config from cache (not DB!)
	config, err := GetWebhookConfig(instanceID)
	if err != nil || config.URL == "" {
		return
	}

	payload := WebhookPayload{
		Event:     "incoming_message",
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("webhook: marshal error: %v", err)
		return
	}

	timestampHeader, signatureHeader := signWebhookBody(config.Secret, body)

	go deliverWebhook(config.URL, body, timestampHeader, signatureHeader, instanceID)
}

// signWebhookBody returns the HMAC-SHA256 of "timestamp.body" with its
// timestamp. An empty secret disables signing and returns two empty strings.
func signWebhookBody(secret string, body []byte) (string, string) {
	if secret == "" {
		return "", ""
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return ts, hex.EncodeToString(mac.Sum(nil))
}

// postWebhookOnce performs one delivery attempt. It reports whether the caller
// should stop retrying.
func postWebhookOnce(url string, body []byte, timestampHeader, signatureHeader string, attempt int) bool {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Printf("webhook: new request error: %v", err)
		return true
	}
	req.Header.Set("Content-Type", "application/json")
	if signatureHeader != "" {
		req.Header.Set("X-Charon-Timestamp", timestampHeader)
		req.Header.Set("X-Charon-Signature", signatureHeader)
	}

	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		log.Printf("webhook: attempt %d send error: %v", attempt, err)
		return false
	}

	status := resp.StatusCode
	_ = resp.Body.Close()
	// Success (2xx) or a client error the receiver owns (4xx) — stop retrying.
	if status < 500 {
		return true
	}
	log.Printf("webhook: attempt %d returned status %d, will retry", attempt, status)
	return false
}

// deliverWebhook posts the payload, retrying a server-side failure on the
// configured backoff schedule.
func deliverWebhook(url string, body []byte, timestampHeader, signatureHeader, instanceID string) {
	attempts := len(webhookRetryBackoffs) + 1
	for i := range attempts {
		if postWebhookOnce(url, body, timestampHeader, signatureHeader, i+1) {
			return
		}
		if i < len(webhookRetryBackoffs) {
			time.Sleep(webhookRetryBackoffs[i])
		}
	}
	log.Printf("webhook: giving up after %d attempts for instance %s", attempts, instanceID)
}

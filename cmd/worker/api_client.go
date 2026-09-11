package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// sharedHTTPClient is reused across all outbound API calls so we pool
// TCP connections and every request shares a bounded total timeout.
var sharedHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// instancesCacheTTL is how long GetInstances serves the cached instance list
// before it calls the API again.
const instancesCacheTTL = 1 * time.Minute

type InstanceInfo struct {
	InstanceID  string `json:"instanceId"`
	PhoneNumber string `json:"phoneNumber"`
}

// instanceEntry is one row of the API's instance list.
type instanceEntry struct {
	InstanceID  string `json:"instanceId"`
	PhoneNumber string `json:"phoneNumber"`
	Used        bool   `json:"used"`
	Circle      string `json:"circle"`
	Status      string `json:"status"`
}

type CharonClient struct {
	BaseURL string
	APIKey  string

	// Caching instances to reduce API load
	mu                sync.RWMutex
	allInstancesCache []instanceEntry
	cacheExpiry       time.Time
}

type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Instances []instanceEntry `json:"instances"`
	} `json:"data"`
}

func NewCharonClient(baseURL, apiKey string) *CharonClient {
	return &CharonClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
	}
}

func (c *CharonClient) setAuthHeader(req *http.Request) {
	req.Header.Set("X-API-Key", c.APIKey)
}

// do sends req and returns the response. Every outbound call in this file goes
// through it, so the SSRF justification lives in one place.
//
// The host and scheme come from c.BaseURL, which main reads once from
// OUTBOX_API_BASEURL at startup. Callers only append a path this file builds,
// and every path segment they interpolate is escaped, so no request data can
// redirect the call to another host.
func (c *CharonClient) do(req *http.Request) (*http.Response, error) {
	c.setAuthHeader(req)
	// #nosec G704
	return sharedHTTPClient.Do(req)
}

// postJSON posts payload to path and returns the decoded success flag and message.
func (c *CharonClient) postJSON(ctx context.Context, path string, payload map[string]string) (bool, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return false, "", err
	}

	// See CharonClient.do for why the target host cannot be influenced here.
	// #nosec G704
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", err
	}

	var res APIResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, string(raw), err
	}

	return res.Success, res.Message, nil
}

// cachedInstances returns the instances of one circle from the cache. The second
// result is false when the cache is empty or expired.
func (c *CharonClient) cachedInstances(circle string) ([]InstanceInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.allInstancesCache == nil || !time.Now().Before(c.cacheExpiry) {
		return nil, false
	}
	return filterInstancesByCircle(c.allInstancesCache, circle), true
}

// filterInstancesByCircle keeps the in-use instances of one circle.
func filterInstancesByCircle(all []instanceEntry, circle string) []InstanceInfo {
	var instances []InstanceInfo
	for _, inst := range all {
		if inst.Used && inst.Circle == circle {
			instances = append(instances, InstanceInfo{
				InstanceID:  inst.InstanceID,
				PhoneNumber: inst.PhoneNumber,
			})
		}
	}
	return instances
}

func (c *CharonClient) GetInstances(ctx context.Context, circle string) ([]InstanceInfo, error) {
	if instances, ok := c.cachedInstances(circle); ok {
		return instances, nil
	}

	// See CharonClient.do for why the target host cannot be influenced here.
	// #nosec G704
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/instances?all=true", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var res APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.allInstancesCache = res.Data.Instances
	c.cacheExpiry = time.Now().Add(instancesCacheTTL)
	c.mu.Unlock()

	return filterInstancesByCircle(res.Data.Instances, circle), nil
}

func (c *CharonClient) SendMessage(ctx context.Context, instanceID, to, message string) (bool, string, error) {
	path := fmt.Sprintf("/api/send/%s", url.PathEscape(instanceID))
	return c.postJSON(ctx, path, map[string]string{
		"to":      to,
		"message": message,
	})
}

func (c *CharonClient) SendGroupMessage(ctx context.Context, instanceID, groupID, message string) (bool, string, error) {
	path := fmt.Sprintf("/api/send-group/%s", url.PathEscape(instanceID))
	return c.postJSON(ctx, path, map[string]string{
		"message":  message,
		"groupJid": groupID,
	})
}

func (c *CharonClient) SendMediaURL(ctx context.Context, instanceID, to, mediaURL, caption string) (bool, string, error) {
	path := fmt.Sprintf("/api/send/%s/media-url", url.PathEscape(instanceID))
	return c.postJSON(ctx, path, map[string]string{
		"to":       to,
		"mediaUrl": mediaURL,
		"caption":  caption,
	})
}

func (c *CharonClient) SendGroupMediaURL(ctx context.Context, instanceID, groupID, mediaURL, caption string) (bool, string, error) {
	path := fmt.Sprintf("/api/send-group/%s/media-url", url.PathEscape(instanceID))
	return c.postJSON(ctx, path, map[string]string{
		"groupJid": groupID,
		"mediaUrl": mediaURL,
		"message":  caption,
	})
}

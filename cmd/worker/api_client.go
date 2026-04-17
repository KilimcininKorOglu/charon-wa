package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// sharedHTTPClient is reused across all outbound API calls so we pool
// TCP connections and every request shares a bounded total timeout.
var sharedHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

type InstanceInfo struct {
	InstanceID  string `json:"instanceId"`
	PhoneNumber string `json:"phoneNumber"`
}

type CharonClient struct {
	BaseURL string
	APIKey  string

	// Caching instances to reduce API load
	mu                sync.RWMutex
	allInstancesCache []struct {
		InstanceID  string `json:"instanceId"`
		PhoneNumber string `json:"phoneNumber"`
		Used        bool   `json:"used"`
		Circle      string `json:"circle"`
		Status      string `json:"status"`
	}
	cacheExpiry time.Time
}

type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Instances []struct {
			InstanceID  string `json:"instanceId"`
			PhoneNumber string `json:"phoneNumber"`
			Used        bool   `json:"used"`
			Circle      string `json:"circle"`
			Status      string `json:"status"`
		} `json:"instances"`
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

func (c *CharonClient) GetInstances(ctx context.Context, circle string) ([]InstanceInfo, error) {
	c.mu.RLock()
	if c.allInstancesCache != nil && time.Now().Before(c.cacheExpiry) {
		var instances []InstanceInfo
		for _, inst := range c.allInstancesCache {
			if inst.Used && inst.Circle == circle {
				instances = append(instances, InstanceInfo{
					InstanceID:  inst.InstanceID,
					PhoneNumber: inst.PhoneNumber,
				})
			}
		}
		c.mu.RUnlock()
		return instances, nil
	}
	c.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/api/instances?all=true", nil)
	if err != nil {
		return nil, err
	}
	c.setAuthHeader(req)

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.allInstancesCache = res.Data.Instances
	c.cacheExpiry = time.Now().Add(1 * time.Minute)
	c.mu.Unlock()

	var instances []InstanceInfo
	for _, inst := range res.Data.Instances {
		if inst.Used && inst.Circle == circle {
			instances = append(instances, InstanceInfo{
				InstanceID:  inst.InstanceID,
				PhoneNumber: inst.PhoneNumber,
			})
		}
	}

	return instances, nil
}

func (c *CharonClient) SendMessage(ctx context.Context, instanceID, to, message string) (bool, string, error) {
	payload, _ := json.Marshal(map[string]string{
		"to":      to,
		"message": message,
	})

	url := fmt.Sprintf("%s/api/send/%s", c.BaseURL, instanceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return false, "", err
	}
	c.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res APIResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return false, string(body), err
	}

	return res.Success, res.Message, nil
}

func (c *CharonClient) SendGroupMessage(ctx context.Context, instanceID, groupID, message string) (bool, string, error) {
	payload, _ := json.Marshal(map[string]string{
		"message":  message,
		"groupJid": groupID,
	})

	url := fmt.Sprintf("%s/api/send-group/%s", c.BaseURL, instanceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return false, "", err
	}
	c.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res APIResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return false, string(body), err
	}

	return res.Success, res.Message, nil
}

func (c *CharonClient) SendMediaURL(ctx context.Context, instanceID, to, mediaURL, caption string) (bool, string, error) {
	payload, _ := json.Marshal(map[string]string{
		"to":       to,
		"mediaUrl": mediaURL,
		"caption":  caption,
	})

	url := fmt.Sprintf("%s/api/send/%s/media-url", c.BaseURL, instanceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return false, "", err
	}
	c.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res APIResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return false, string(body), err
	}

	return res.Success, res.Message, nil
}

func (c *CharonClient) SendGroupMediaURL(ctx context.Context, instanceID, groupID, mediaURL, caption string) (bool, string, error) {
	payload, _ := json.Marshal(map[string]string{
		"groupJid": groupID,
		"mediaUrl": mediaURL,
		"message":  caption,
	})

	url := fmt.Sprintf("%s/api/send-group/%s/media-url", c.BaseURL, instanceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return false, "", err
	}
	c.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res APIResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return false, string(body), err
	}

	return res.Success, res.Message, nil
}

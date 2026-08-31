package outpost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type PublishResult struct {
	EventID        string
	Duplicate      bool
	DestinationIDs []string
	StatusCode     int
	Latency        time.Duration
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1200,
				MaxIdleConnsPerHost: 1200,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (c *Client) CreateTenant(ctx context.Context, tenantID string) error {
	req, err := c.newRequest(ctx, http.MethodPut, fmt.Sprintf("/tenants/%s", tenantID), nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("create tenant %s: %w", tenantID, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create tenant %s: status %d", tenantID, resp.StatusCode)
	}
	return nil
}

func (c *Client) DeleteTenant(ctx context.Context, tenantID string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/tenants/%s", tenantID), nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete tenant %s: %w", tenantID, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete tenant %s: status %d", tenantID, resp.StatusCode)
	}
	return nil
}

type createDestinationRequest struct {
	Type   string         `json:"type"`
	Topics any            `json:"topics"`
	Config map[string]any `json:"config"`
}

type createDestinationResponse struct {
	ID string `json:"id"`
}

func (c *Client) CreateDestination(ctx context.Context, tenantID string, topics []string, webhookURL string) (string, error) {
	body := createDestinationRequest{
		Type:   "webhook",
		Topics: topics,
		Config: map[string]interface{}{
			"url": webhookURL,
		},
	}

	req, err := c.newRequest(ctx, http.MethodPost, fmt.Sprintf("/tenants/%s/destinations", tenantID), body)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create destination for %s: %w", tenantID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create destination for %s: status %d: %s", tenantID, resp.StatusCode, string(respBody))
	}

	var result createDestinationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("create destination for %s: decode: %w", tenantID, err)
	}
	return result.ID, nil
}

type publishRequest struct {
	ID       string          `json:"id"`
	TenantID string          `json:"tenant_id"`
	Topic    string          `json:"topic"`
	Data     json.RawMessage `json:"data"`
}

type publishResponse struct {
	ID             string   `json:"id"`
	Duplicate      bool     `json:"duplicate"`
	DestinationIDs []string `json:"destination_ids"`
}

func (c *Client) Publish(ctx context.Context, tenantID, eventID, topic string, data []byte) (PublishResult, error) {
	body := publishRequest{
		ID:       eventID,
		TenantID: tenantID,
		Topic:    topic,
		Data:     json.RawMessage(data),
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/publish", body)

	if err != nil {
		return PublishResult{}, err
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return PublishResult{}, fmt.Errorf("publish event %s: %w", eventID, err)
	}
	defer resp.Body.Close()

	result := PublishResult{
		EventID:    eventID,
		StatusCode: resp.StatusCode,
		Latency:    latency,
	}

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
		var pr publishResponse
		if err := json.NewDecoder(resp.Body).Decode(&pr); err == nil {
			result.EventID = pr.ID
			result.Duplicate = pr.Duplicate
			result.DestinationIDs = pr.DestinationIDs
		}
	} else {
		io.Copy(io.Discard, resp.Body)
		return result, fmt.Errorf("publish event %s: status %d", eventID, resp.StatusCode)
	}

	return result, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

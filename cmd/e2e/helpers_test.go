package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
)

const (
	testSecret    = "testsecret1234567890abcdefghijklmnop"
	testSecretAlt = "testsecret0987654321zyxwvutsrqponm"
)

// envDuration reads a duration from an environment variable, falling back to a default.
func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// Centralized poll timeouts — override via environment for slow CI.
var (
	mockServerPollTimeout = envDuration("E2E_MOCK_TIMEOUT", 10*time.Second)
	attemptPollTimeout    = envDuration("E2E_ATTEMPT_TIMEOUT", 10*time.Second)
	alertPollTimeout      = envDuration("E2E_ALERT_TIMEOUT", 10*time.Second)
)

// =============================================================================
// Response structs (test-specific, not reusing internal/models)
// =============================================================================

type tenantResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type destinationResponse struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	Type        string            `json:"type"`
	Topics      json.RawMessage   `json:"topics"`
	Config      map[string]string `json:"config"`
	Credentials map[string]string `json:"credentials"`
	DisabledAt  *string           `json:"disabled_at"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

type publishResponse struct {
	ID             string   `json:"id"`
	Duplicate      bool     `json:"duplicate"`
	DestinationIDs []string `json:"destination_ids"`
}

type mockServerEvent struct {
	Success  bool                   `json:"success"`
	Verified bool                   `json:"verified"`
	Payload  map[string]interface{} `json:"payload"`
}

// =============================================================================
// Mock destination wrapper
// =============================================================================

type webhookDestination struct {
	destinationResponse
	mockID string // destination ID on mock server
}

// SetResponse reconfigures the mock server to return a specific HTTP status code.
func (d *webhookDestination) SetResponse(s *basicSuite, status int) {
	s.T().Helper()
	s.doJSON(http.MethodPut, s.mockServerURL()+"/destinations", map[string]any{
		"id":   d.mockID,
		"type": "webhook",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", s.mockServerURL(), d.mockID),
		},
		"response": map[string]any{
			"status": status,
		},
	}, nil)
}

// SetSecret updates the mock server's secret for signature verification.
func (d *webhookDestination) SetSecret(s *basicSuite, secret string) {
	s.T().Helper()
	s.doJSON(http.MethodPut, s.mockServerURL()+"/destinations", map[string]any{
		"id":   d.mockID,
		"type": "webhook",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", s.mockServerURL(), d.mockID),
		},
		"credentials": map[string]any{
			"secret": secret,
		},
	}, nil)
}

// SetCredentials updates the mock server's full credentials.
func (d *webhookDestination) SetCredentials(s *basicSuite, creds map[string]string) {
	s.T().Helper()
	credMap := make(map[string]any, len(creds))
	for k, v := range creds {
		credMap[k] = v
	}
	s.doJSON(http.MethodPut, s.mockServerURL()+"/destinations", map[string]any{
		"id":   d.mockID,
		"type": "webhook",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", s.mockServerURL(), d.mockID),
		},
		"credentials": credMap,
	}, nil)
}

// =============================================================================
// Internal HTTP helpers
// =============================================================================

// doJSON sends a request with admin API key auth. Returns status code.
// Fails test on transport/marshal errors. result can be nil to discard body.
func (s *basicSuite) doJSON(method, url string, body any, result any) int {
	s.T().Helper()
	return s.doJSONWithAuth(method, url, fmt.Sprintf("Bearer %s", s.config.APIKey), body, result)
}

// doJSONRaw sends a request without any auth header.
func (s *basicSuite) doJSONRaw(method, url string, body any, result any) int {
	s.T().Helper()
	return s.doJSONWithAuth(method, url, "", body, result)
}

func (s *basicSuite) doJSONWithAuth(method, url string, authHeader string, body any, result any) int {
	s.T().Helper()

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		s.Require().NoError(err)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	s.Require().NoError(err)
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := s.httpClient.Do(req)
	s.Require().NoError(err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)

	// Log response body on non-2xx when caller doesn't inspect it (aids CI debugging).
	if result == nil && resp.StatusCode >= 400 && len(respBody) > 0 {
		s.T().Logf("HTTP %d %s %s: %s", resp.StatusCode, method, url, respBody)
	}

	if result != nil && len(respBody) > 0 {
		s.Require().NoError(json.Unmarshal(respBody, result))
	}

	return resp.StatusCode
}

// apiURL builds a full URL for the outpost API.
func (s *basicSuite) apiURL(path string) string {
	return fmt.Sprintf("http://localhost:%d/api/v1%s", s.config.APIPort, path)
}

// mockServerURL returns the mock server base URL.
func (s *basicSuite) mockServerURL() string {
	return s.mockServerBaseURL
}

// =============================================================================
// Resource helpers
// =============================================================================

// createTenant creates a new tenant with a random ID.
func (s *basicSuite) createTenant() tenantResponse {
	s.T().Helper()
	id := idgen.String()
	var resp tenantResponse
	status := s.doJSON(http.MethodPut, s.apiURL("/tenants/"+id), nil, &resp)
	s.Require().Equal(http.StatusCreated, status, "failed to create tenant %s", id)
	return resp
}

// createWebhookDestination registers on mock server and creates on outpost.
func (s *basicSuite) createWebhookDestination(tenantID, topic string, opts ...destOpt) *webhookDestination {
	s.T().Helper()

	o := destOpts{}
	for _, fn := range opts {
		fn(&o)
	}

	destID := idgen.Destination()

	// Register on mock server
	mockBody := map[string]any{
		"id":   destID,
		"type": "webhook",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", s.mockServerURL(), destID),
		},
	}
	if o.secret != "" {
		mockBody["credentials"] = map[string]any{
			"secret": o.secret,
		}
	}
	if o.responseStatus != 0 {
		mockBody["response"] = map[string]any{
			"status": o.responseStatus,
		}
	}

	status := s.doJSONRaw(http.MethodPut, s.mockServerURL()+"/destinations", mockBody, nil)
	s.Require().Equal(http.StatusOK, status, "failed to register mock destination %s", destID)

	// Create on outpost
	outpostBody := map[string]any{
		"id":     destID,
		"type":   "webhook",
		"topics": []string{topic},
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", s.mockServerURL(), destID),
		},
	}
	if o.secret != "" {
		outpostBody["credentials"] = map[string]any{
			"secret": o.secret,
		}
	}
	if o.filter != nil {
		outpostBody["filter"] = o.filter
	}

	var resp destinationResponse
	status = s.doJSON(http.MethodPost, s.apiURL("/tenants/"+tenantID+"/destinations"), outpostBody, &resp)
	s.Require().Equal(http.StatusCreated, status, "failed to create destination %s", destID)

	return &webhookDestination{
		destinationResponse: resp,
		mockID:              destID,
	}
}

// publish publishes an event.
func (s *basicSuite) publish(tenantID, topic string, data map[string]any, opts ...publishOpt) publishResponse {
	s.T().Helper()

	o := publishOpts{}
	for _, fn := range opts {
		fn(&o)
	}

	body := map[string]any{
		"tenant_id":          tenantID,
		"topic":              topic,
		"eligible_for_retry": o.eligibleForRetry,
		"data":               data,
	}
	if o.eventID != "" {
		body["id"] = o.eventID
	}
	if o.metadata != nil {
		body["metadata"] = o.metadata
	}
	if o.time != nil {
		body["time"] = o.time.Format(time.RFC3339Nano)
	}

	var resp publishResponse
	status := s.doJSON(http.MethodPost, s.apiURL("/publish"), body, &resp)
	s.Require().Equal(http.StatusAccepted, status, "failed to publish event")
	return resp
}

// getDestination returns a destination.
func (s *basicSuite) getDestination(tenantID, destID string) destinationResponse {
	s.T().Helper()
	var resp destinationResponse
	status := s.doJSON(http.MethodGet, s.apiURL(fmt.Sprintf("/tenants/%s/destinations/%s", tenantID, destID)), nil, &resp)
	s.Require().Equal(http.StatusOK, status, "failed to get destination %s", destID)
	return resp
}

// disableDestination disables a destination.
func (s *basicSuite) disableDestination(tenantID, destID string) {
	s.T().Helper()
	status := s.doJSON(http.MethodPut, s.apiURL(fmt.Sprintf("/tenants/%s/destinations/%s/disable", tenantID, destID)), nil, nil)
	s.Require().Equal(http.StatusOK, status, "failed to disable destination %s", destID)
}

// enableDestination enables a destination.
func (s *basicSuite) enableDestination(tenantID, destID string) {
	s.T().Helper()
	status := s.doJSON(http.MethodPut, s.apiURL(fmt.Sprintf("/tenants/%s/destinations/%s/enable", tenantID, destID)), nil, nil)
	s.Require().Equal(http.StatusOK, status, "failed to enable destination %s", destID)
}

// retryEvent retries an event. Returns status code (caller asserts).
func (s *basicSuite) retryEvent(eventID, destID string) int {
	s.T().Helper()
	return s.doJSON(http.MethodPost, s.apiURL("/retry"), map[string]any{
		"event_id":       eventID,
		"destination_id": destID,
	}, nil)
}

// =============================================================================
// Wait helpers
// =============================================================================

// waitForAttempts polls until at least minCount attempts exist for the tenant.
func (s *basicSuite) waitForNewAttempts(tenantID string, minCount int) []map[string]any {
	s.T().Helper()
	timeout := attemptPollTimeout
	deadline := time.Now().Add(timeout)
	var lastCount int

	for time.Now().Before(deadline) {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+tenantID), nil, &resp)
		if status == http.StatusOK {
			lastCount = len(resp.Models)
			if lastCount >= minCount {
				return resp.Models
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for %d attempts (got %d)", minCount, lastCount)
	return nil
}

// waitForMockServerEvents polls the mock server until at least minCount events exist.
func (s *basicSuite) waitForNewMockServerEvents(destID string, minCount int) []mockServerEvent {
	s.T().Helper()
	timeout := mockServerPollTimeout
	deadline := time.Now().Add(timeout)
	var lastCount int

	for time.Now().Before(deadline) {
		events, ok := s.fetchMockServerEvents(destID)
		if ok {
			lastCount = len(events)
			if lastCount >= minCount {
				return events
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for %d mock events for %s (got %d)", minCount, destID, lastCount)
	return nil
}

// waitForDestinationDisabled polls until the destination has disabled_at set.
func (s *basicSuite) waitForNewDestinationDisabled(tenantID, destID string) {
	s.T().Helper()
	timeout := mockServerPollTimeout
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		dest := s.getDestination(tenantID, destID)
		if dest.DisabledAt != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for destination %s to be disabled", destID)
}

// waitForAlerts polls until at least count alerts exist for the destination.
func (s *basicSuite) waitForAlerts(destID string, count int) {
	s.T().Helper()
	timeout := alertPollTimeout
	deadline := time.Now().Add(timeout)
	var lastCount int

	for time.Now().Before(deadline) {
		lastCount = len(s.alertServer.GetAlertsForDestination(destID))
		if lastCount >= count {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for %d alerts for %s (got %d)", count, destID, lastCount)
}

// waitForAlertsByTopic polls until at least count alerts with the specific topic exist for the destination.
func (s *basicSuite) waitForAlertsByTopic(destID, topic string, count int) {
	s.T().Helper()
	timeout := alertPollTimeout
	deadline := time.Now().Add(timeout)
	var lastCount int

	for time.Now().Before(deadline) {
		lastCount = len(s.alertServer.GetAlertsForDestinationByTopic(destID, topic))
		if lastCount >= count {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for %d %s alerts for %s (got %d)", count, topic, destID, lastCount)
}

// =============================================================================
// Absence assertion
// =============================================================================

// assertNoDelivery sleeps for the given duration then asserts the mock server
// received zero events for the destination.
func (s *basicSuite) assertNoDelivery(destID string, timeout time.Duration) {
	s.T().Helper()
	time.Sleep(timeout)

	events, ok := s.fetchMockServerEvents(destID)
	if !ok {
		// No events endpoint returned non-200 (e.g. 400 "no events found") — means zero events.
		return
	}
	s.Require().Empty(events, "expected no deliveries for destination %s but got %d", destID, len(events))
}

// =============================================================================
// Mock server helpers
// =============================================================================

// fetchMockServerEvents fetches events from the mock server without failing the
// test on non-200 responses. Returns (events, true) on success, (nil, false) on
// non-200 (e.g. 400 "no events found for destination").
func (s *basicSuite) fetchMockServerEvents(destID string) ([]mockServerEvent, bool) {
	resp, err := s.httpClient.Get(s.mockServerURL() + "/destinations/" + destID + "/events")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	var events []mockServerEvent
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, false
	}
	return events, true
}

// clearMockServerEvents clears events for a destination on the mock server.
func (s *basicSuite) clearMockServerEvents(destID string) {
	s.T().Helper()
	status := s.doJSONRaw(http.MethodDelete, s.mockServerURL()+"/destinations/"+destID+"/events", nil, nil)
	s.Require().Equal(http.StatusOK, status, "failed to clear mock server events for %s", destID)
}

// =============================================================================
// Functional options
// =============================================================================

// Destination options
type destOpt func(*destOpts)

type destOpts struct {
	secret         string
	filter         map[string]any
	responseStatus int
}

func withSecret(s string) destOpt {
	return func(o *destOpts) { o.secret = s }
}

func withFilter(f map[string]any) destOpt {
	return func(o *destOpts) { o.filter = f }
}

func withResponseStatus(code int) destOpt {
	return func(o *destOpts) { o.responseStatus = code }
}

// Publish options
type publishOpt func(*publishOpts)

type publishOpts struct {
	eventID          string
	eligibleForRetry bool
	metadata         map[string]string
	time             *time.Time
}

func withEventID(id string) publishOpt {
	return func(o *publishOpts) { o.eventID = id }
}

func withRetry() publishOpt {
	return func(o *publishOpts) { o.eligibleForRetry = true }
}

func withPublishMetadata(m map[string]string) publishOpt {
	return func(o *publishOpts) { o.metadata = m }
}

func withTime(t time.Time) publishOpt {
	return func(o *publishOpts) { o.time = &t }
}

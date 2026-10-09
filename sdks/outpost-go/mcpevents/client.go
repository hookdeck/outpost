package mcpevents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTimeout bounds each call. It is above Outpost's own 20s
	// subscribe deadline: subscribe verifies the callback synchronously.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxResponseBytes is the largest response body accepted.
	DefaultMaxResponseBytes = 8 << 20

	maxErrorBody = 1024
)

// HTTPClient sends the requests. *http.Client and the outpostgo.HTTPClient
// implementations satisfy it.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config configures a Client.
type Config struct {
	// ServerURL is the Outpost API base URL, for example
	// http://localhost:3333/api/v2 (the same value given to
	// outpostgo.WithServerURL). The MCP endpoints exist in API v2 only, so a
	// URL ending in /api/v1 is rewritten to /api/v2.
	ServerURL string
	// APIKey is the Outpost API key (the admin key on self-hosted Outpost).
	APIKey string
	// APIKeySource, when set, is called on each request instead of APIKey.
	APIKeySource func(ctx context.Context) (string, error)
	// HTTPClient defaults to an *http.Client that never follows redirects.
	// A supplied client should not follow them either: a response reached
	// through a redirect is rejected, but the API key was already sent.
	HTTPClient HTTPClient
	// Timeout bounds each call. Defaults to DefaultTimeout.
	Timeout time.Duration
	// MaxResponseBytes defaults to DefaultMaxResponseBytes.
	MaxResponseBytes int64
}

// ListEventsQuery are the query parameters of the event list endpoint.
type ListEventsQuery struct {
	// Cursor is the cursor from the MCP events/list request.
	Cursor string
	// Topics are the topics the principal may discover. nil means no filter;
	// a non-nil empty slice means no topics (Outpost answers with no events).
	Topics []string
	// Limit is the page size, 1 to 100. Zero uses Outpost's default (100).
	Limit int
}

// SubscribeRequest is the body of the subscribe endpoint.
type SubscribeRequest struct {
	// Principal is the authenticated subject of the MCP request.
	Principal string
	// Params are the MCP events/subscribe params, untouched. A JSON object.
	Params json.RawMessage
	// Filter, when set, replaces the filter Outpost generates from the
	// arguments. A JSON object.
	Filter json.RawMessage
	// Metadata is copied to the subscription's destination.
	Metadata map[string]string
	// AllowedTopics, when non-nil (even empty), makes Outpost answer
	// not_found for a params.name outside the list.
	AllowedTopics []string
}

// UnsubscribeRequest is the body of the unsubscribe endpoint.
type UnsubscribeRequest struct {
	Principal string
	// Params are the MCP events/unsubscribe params, untouched. A JSON object.
	Params json.RawMessage
}

// API is the three Outpost calls the handlers use. *Client implements it.
// Results are the MCP results as raw JSON objects; MCP failures return an
// *MCPError.
type API interface {
	ListEvents(ctx context.Context, tenantID string, q ListEventsQuery) (json.RawMessage, error)
	Subscribe(ctx context.Context, tenantID string, req SubscribeRequest) (json.RawMessage, error)
	Unsubscribe(ctx context.Context, tenantID string, req UnsubscribeRequest) (json.RawMessage, error)
}

// Client is a raw-JSON client for the Outpost MCP endpoints. It never
// reshapes params or results, so fields the protocol adds later pass through.
// It is safe for concurrent use.
type Client struct {
	base         string
	apiKey       string
	apiKeySource func(context.Context) (string, error)
	httpClient   HTTPClient
	timeout      time.Duration
	maxBytes     int64
}

var _ API = (*Client)(nil)

// NewClient returns a Client for cfg.
func NewClient(cfg Config) (*Client, error) {
	base, err := normalizeBaseURL(cfg.ServerURL)
	if err != nil {
		return nil, err
	}
	c := &Client{
		base:         base,
		apiKey:       cfg.APIKey,
		apiKeySource: cfg.APIKeySource,
		httpClient:   cfg.HTTPClient,
		timeout:      cfg.Timeout,
		maxBytes:     cfg.MaxResponseBytes,
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{
			// Outpost never redirects; following one would replay the API key.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if c.timeout <= 0 {
		c.timeout = DefaultTimeout
	}
	if c.maxBytes <= 0 {
		c.maxBytes = DefaultMaxResponseBytes
	}
	return c, nil
}

// BaseURL is the URL the endpoints are resolved against (always API v2).
func (c *Client) BaseURL() string { return c.base }

// ListEvents calls GET /tenants/{tenant_id}/mcp/events and returns the
// events/list result.
func (c *Client) ListEvents(ctx context.Context, tenantID string, q ListEventsQuery) (json.RawMessage, error) {
	query := url.Values{}
	if q.Cursor != "" {
		query.Set("cursor", q.Cursor)
	}
	if q.Topics != nil {
		// Present but empty means "no topics" to Outpost, so an empty
		// allowlist never widens to the whole catalog.
		query.Set("topics", strings.Join(q.Topics, ","))
	}
	if q.Limit > 0 {
		query.Set("limit", strconv.Itoa(q.Limit))
	}
	return c.call(ctx, http.MethodGet, tenantID, "mcp/events", query, nil)
}

// Subscribe calls PUT /tenants/{tenant_id}/mcp/subscriptions and returns the
// events/subscribe result.
func (c *Client) Subscribe(ctx context.Context, tenantID string, req SubscribeRequest) (json.RawMessage, error) {
	if !isJSONObject(req.Params) {
		return nil, errors.New("mcpevents: subscribe params must be a JSON object")
	}
	if len(req.Filter) > 0 && !isJSONObject(req.Filter) {
		return nil, errors.New("mcpevents: subscribe filter must be a JSON object")
	}
	body := subscribeBody{
		Principal: req.Principal,
		Params:    req.Params,
		Filter:    req.Filter,
		Metadata:  req.Metadata,
	}
	if req.AllowedTopics != nil {
		body.AllowedTopics = &req.AllowedTopics
	}
	return c.call(ctx, http.MethodPut, tenantID, "mcp/subscriptions", nil, body)
}

// Unsubscribe calls POST /tenants/{tenant_id}/mcp/subscriptions/unsubscribe.
// The result is always {}.
func (c *Client) Unsubscribe(ctx context.Context, tenantID string, req UnsubscribeRequest) (json.RawMessage, error) {
	if !isJSONObject(req.Params) {
		return nil, errors.New("mcpevents: unsubscribe params must be a JSON object")
	}
	body := unsubscribeBody{Principal: req.Principal, Params: req.Params}
	return c.call(ctx, http.MethodPost, tenantID, "mcp/subscriptions/unsubscribe", nil, body)
}

type subscribeBody struct {
	Principal     string            `json:"principal"`
	Params        json.RawMessage   `json:"params"`
	Filter        json.RawMessage   `json:"filter,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTopics *[]string         `json:"allowed_topics,omitempty"`
}

type unsubscribeBody struct {
	Principal string          `json:"principal"`
	Params    json.RawMessage `json:"params"`
}

func (c *Client) call(ctx context.Context, method, tenantID, path string, query url.Values, body any) (json.RawMessage, error) {
	segment, err := escapeTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	target := c.base + "tenants/" + segment + "/" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var payload io.Reader
	if body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return nil, fmt.Errorf("mcpevents: encoding request: %w", err)
		}
		payload = &buf
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return nil, fmt.Errorf("mcpevents: building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// identity: the size cap then applies to what is actually buffered.
	req.Header.Set("Accept-Encoding", "identity")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	key := c.apiKey
	if c.apiKeySource != nil {
		if key, err = c.apiKeySource(ctx); err != nil {
			return nil, fmt.Errorf("mcpevents: api key: %w", err)
		}
	}
	if key != "" {
		if len(key) < 7 || !strings.EqualFold(key[:7], "bearer ") {
			key = "Bearer " + key
		}
		req.Header.Set("Authorization", key)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &RequestError{msg: "outpost: request failed", Err: err}
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.String() != req.URL.String() {
		return nil, &RequestError{msg: "outpost: request was redirected", StatusCode: resp.StatusCode}
	}
	if resp.ContentLength > c.maxBytes {
		return nil, &RequestError{msg: "outpost: response is too large", StatusCode: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return nil, &RequestError{msg: "outpost: reading response", StatusCode: resp.StatusCode, Err: err}
	}
	if int64(len(data)) > c.maxBytes {
		return nil, &RequestError{msg: "outpost: response is too large", StatusCode: resp.StatusCode}
	}
	return parseResponse(resp.StatusCode, data)
}

func parseResponse(status int, data []byte) (json.RawMessage, error) {
	if status >= 200 && status < 300 {
		if trimmed := bytes.TrimSpace(data); isJSONObject(trimmed) {
			return json.RawMessage(trimmed), nil
		}
		return nil, &RequestError{msg: "outpost: malformed MCP result", StatusCode: status, Body: truncate(data)}
	}
	if status == http.StatusUnprocessableEntity {
		if m := parseMCPErrorBody(data, status); m != nil {
			return nil, m
		}
	}
	return nil, &RequestError{msg: "outpost: responded with status " + strconv.Itoa(status), StatusCode: status, Body: truncate(data)}
}

func normalizeBaseURL(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("mcpevents: the server URL must be an absolute http or https URL")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if strings.HasSuffix(path, "/api/v1") {
		path = strings.TrimSuffix(path, "/api/v1") + "/api/v2"
	}
	return u.Scheme + "://" + u.Host + path + "/", nil
}

// escapeTenantID percent-encodes a tenant ID as one path segment. "." and
// ".." are rejected: URL normalization would resolve them as dot segments.
func escapeTenantID(tenantID string) (string, error) {
	if tenantID == "" || tenantID == "." || tenantID == ".." {
		return "", errors.New("mcpevents: invalid tenant ID")
	}
	return url.PathEscape(tenantID), nil
}

func isJSONObject(b json.RawMessage) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && b[0] == '{' && json.Valid(b)
}

func truncate(b []byte) string {
	if len(b) > maxErrorBody {
		b = b[:maxErrorBody]
	}
	return strings.ToValidUTF8(string(b), "")
}

package mcpevents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mcpErrorBody = `{"mcp_error":{"kind":"callback_endpoint_error","code":-32015,"message":"CallbackEndpointError","data":{"reason":"challenge_failed"}}}`

type recorded struct {
	Method  string
	RawPath string
	Query   string
	Header  http.Header
	Body    string
}

// outpostStub records requests and answers each with respond.
type outpostStub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recorded
}

func newOutpostStub(t *testing.T, respond http.HandlerFunc) *outpostStub {
	t.Helper()
	s := &outpostStub{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, recorded{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Clone(), string(body)})
		s.mu.Unlock()
		respond(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *outpostStub) last(t *testing.T) recorded {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.requests)
	return s.requests[len(s.requests)-1]
}

func (s *outpostStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func jsonAnswer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func newTestClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := NewClient(cfg)
	require.NoError(t, err)
	return c
}

func TestClientListEvents(t *testing.T) {
	result := `{"events":[{"name":"order.created","inputSchema":{"type":"object"}}],"nextCursor":"dDpi"}`
	s := newOutpostStub(t, jsonAnswer(200, " "+result+"\n"))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2", APIKey: "key"})

	got, err := c.ListEvents(context.Background(), "store/1?x#y", ListEventsQuery{Cursor: "abc", Topics: []string{"a", "b"}, Limit: 5})
	require.NoError(t, err)
	assert.Equal(t, result, string(got), "the result is forwarded byte for byte")

	req := s.last(t)
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, "/api/v2/tenants/store%2F1%3Fx%23y/mcp/events", req.RawPath)
	assert.Equal(t, "cursor=abc&limit=5&topics=a%2Cb", req.Query)
	assert.Equal(t, "Bearer key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
	assert.Equal(t, "identity", req.Header.Get("Accept-Encoding"))
}

func TestClientCursorCannotInjectQueryParameters(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(200, `{"events":[]}`))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})
	_, err := c.ListEvents(context.Background(), "t", ListEventsQuery{Cursor: "x&topics=secret#frag", Topics: []string{"a"}})
	require.NoError(t, err)
	query, err := url.ParseQuery(s.last(t).Query)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, query["topics"])
	assert.Equal(t, "x&topics=secret#frag", query.Get("cursor"))
}

func TestClientEmptyTopicsArePresent(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(200, `{"events":[]}`))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})

	_, err := c.ListEvents(context.Background(), "t", ListEventsQuery{Topics: []string{}})
	require.NoError(t, err)
	assert.Equal(t, "topics=", s.last(t).Query)

	_, err = c.ListEvents(context.Background(), "t", ListEventsQuery{})
	require.NoError(t, err)
	assert.Equal(t, "", s.last(t).Query)
	assert.Empty(t, s.last(t).Header.Get("Authorization"))
}

func TestClientBaseURL(t *testing.T) {
	tests := map[string]string{
		"http://h/api/v1":                             "http://h/api/v2/",
		"http://h/api/v1/":                            "http://h/api/v2/",
		"http://h/prefix/api/v1?q=1#f":                "http://h/prefix/api/v2/",
		"http://h:3333/api/v2":                        "http://h:3333/api/v2/",
		"https://api.outpost.hookdeck.com/2025-07-01": "https://api.outpost.hookdeck.com/2025-07-01/",
		"http://user:pass@h:3333/api/v2":              "http://h:3333/api/v2/",
		"http://[::1]:3333/api/v2":                    "http://[::1]:3333/api/v2/",
	}
	for in, want := range tests {
		c := newTestClient(t, Config{ServerURL: in})
		assert.Equal(t, want, c.BaseURL(), in)
	}
	for _, bad := range []string{"", "file:///etc/passwd", "localhost:3333", "/api/v2", "http://"} {
		_, err := NewClient(Config{ServerURL: bad})
		assert.Error(t, err, bad)
	}
}

func TestClientRejectsDotSegmentTenants(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(200, `{}`))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})
	for _, tenant := range []string{"", ".", ".."} {
		_, err := c.ListEvents(context.Background(), tenant, ListEventsQuery{})
		assert.Error(t, err, tenant)
	}
	assert.Equal(t, 0, s.count())

	_, err := c.ListEvents(context.Background(), "../..%2e/admin", ListEventsQuery{})
	require.NoError(t, err)
	assert.Equal(t, "/api/v2/tenants/..%2F..%252e%2Fadmin/mcp/events", s.last(t).RawPath)
}

func TestClientSubscribe(t *testing.T) {
	result := `{"id":"sub_1","refreshBefore":"2026-10-09T18:00:00Z","cursor":null,"truncated":false,"deliveryStatus":{"active":true,"lastDeliveryAt":null,"lastError":null},"futureField":{"nested":[1,2]}}`
	s := newOutpostStub(t, jsonAnswer(200, result))
	c := newTestClient(t, Config{
		ServerURL:    s.URL + "/api/v2",
		APIKeySource: func(context.Context) (string, error) { return "Bearer already", nil },
	})
	params := `{"name":"order.created","arguments":{"total":{"$gte":100}},"delivery":{"mode":"webhook","url":"https://r.example/cb?a=1&b=<2>","secret":"whsec_x"},"cursor":null,"_meta":{"unknown":true}}`

	got, err := c.Subscribe(context.Background(), "t", SubscribeRequest{
		Principal:     "user_1",
		Params:        json.RawMessage(params),
		Metadata:      map[string]string{"k": "v"},
		AllowedTopics: []string{"order.created"},
	})
	require.NoError(t, err)
	assert.Equal(t, result, string(got))

	req := s.last(t)
	assert.Equal(t, http.MethodPut, req.Method)
	assert.Equal(t, "/api/v2/tenants/t/mcp/subscriptions", req.RawPath)
	assert.Equal(t, "Bearer already", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Contains(t, req.Body, `"params":`+params, "params are sent untouched, without HTML escaping")
	assert.JSONEq(t, `{"principal":"user_1","params":`+params+`,"metadata":{"k":"v"},"allowed_topics":["order.created"]}`, req.Body)
}

func TestClientSubscribeAllowedTopics(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(200, `{}`))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})

	_, err := c.Subscribe(context.Background(), "t", SubscribeRequest{Principal: "p", Params: json.RawMessage(`{}`), AllowedTopics: []string{}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"principal":"p","params":{},"allowed_topics":[]}`, s.last(t).Body, "an empty allowlist is sent")

	_, err = c.Subscribe(context.Background(), "t", SubscribeRequest{Principal: "p", Params: json.RawMessage(`{}`)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"principal":"p","params":{}}`, s.last(t).Body, "a nil allowlist is omitted")
}

func TestClientValidatesBodies(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(200, `{}`))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})
	for _, params := range []string{"", "[]", `"x"`, "{", "null"} {
		_, err := c.Subscribe(context.Background(), "t", SubscribeRequest{Principal: "p", Params: json.RawMessage(params)})
		assert.Error(t, err, params)
		_, err = c.Unsubscribe(context.Background(), "t", UnsubscribeRequest{Principal: "p", Params: json.RawMessage(params)})
		assert.Error(t, err, params)
	}
	_, err := c.Subscribe(context.Background(), "t", SubscribeRequest{Principal: "p", Params: json.RawMessage(`{}`), Filter: json.RawMessage(`[]`)})
	assert.Error(t, err)
	assert.Equal(t, 0, s.count())
}

func TestClientUnsubscribe(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(200, `{}`))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})
	got, err := c.Unsubscribe(context.Background(), "t", UnsubscribeRequest{Principal: "p", Params: json.RawMessage(`{"name":"a"}`)})
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(got))
	assert.Equal(t, http.MethodPost, s.last(t).Method)
	assert.Equal(t, "/api/v2/tenants/t/mcp/subscriptions/unsubscribe", s.last(t).RawPath)
}

func TestClientMCPError(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(422, mcpErrorBody))
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})

	_, err := c.Subscribe(context.Background(), "t", SubscribeRequest{Principal: "p", Params: json.RawMessage(`{}`)})
	var mcpErr *MCPError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, 422, mcpErr.StatusCode)
	assert.Equal(t, "callback_endpoint_error", mcpErr.Kind)
	assert.Equal(t, int64(-32015), mcpErr.Code)
	assert.Equal(t, "CallbackEndpointError", mcpErr.Message)
	assert.JSONEq(t, `{"reason":"challenge_failed"}`, string(mcpErr.Data))
}

func TestClientOtherFailures(t *testing.T) {
	tests := []struct {
		status int
		body   string
	}{
		{422, `{"message":"validation error","data":["x"]}`},
		{422, `{"mcp_error":{"kind":"x","code":"-1","message":"m"}}`},
		{422, `{"mcp_error":{"kind":"x","code":1.5,"message":"m"}}`},
		{422, `{"mcp_error":{"kind":"x","code":-1,"message":null}}`},
		{500, strings.Repeat("x", 5000)},
		{503, ""},
		{302, ""},
		{200, "[]"},
		{200, "not json"},
		{200, ""},
		{200, "null"},
		{200, `{"a":`},
	}
	for _, tt := range tests {
		s := newOutpostStub(t, jsonAnswer(tt.status, tt.body))
		c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2"})
		_, err := c.ListEvents(context.Background(), "t", ListEventsQuery{})
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr, "%d %q", tt.status, tt.body)
		assert.Equal(t, tt.status, reqErr.StatusCode)
		assert.LessOrEqual(t, len(reqErr.Body), 1024)
		_, isMCP := MCPErrorFrom(err)
		assert.False(t, isMCP)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var targetHits int
	var mu sync.Mutex
	s := newOutpostStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			mu.Lock()
			targetHits++
			mu.Unlock()
			jsonAnswer(200, `{"events":[]}`)(w, r)
			return
		}
		http.Redirect(w, r, "/target", http.StatusFound)
	})

	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2", APIKey: "secret"})
	_, err := c.ListEvents(context.Background(), "t", ListEventsQuery{})
	var reqErr *RequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, http.StatusFound, reqErr.StatusCode)
	assert.Equal(t, 0, targetHits)

	// A supplied client that follows redirects gets its answer rejected.
	c = newTestClient(t, Config{ServerURL: s.URL + "/api/v2", HTTPClient: &http.Client{}})
	_, err = c.ListEvents(context.Background(), "t", ListEventsQuery{})
	require.ErrorAs(t, err, &reqErr)
	assert.Contains(t, err.Error(), "redirected")
}

func TestClientResponseLimit(t *testing.T) {
	declared := newOutpostStub(t, jsonAnswer(200, `{"a":"`+strings.Repeat("x", 4096)+`"}`))
	c := newTestClient(t, Config{ServerURL: declared.URL + "/api/v2", MaxResponseBytes: 1024})
	_, err := c.ListEvents(context.Background(), "t", ListEventsQuery{})
	assert.ErrorContains(t, err, "too large")

	streamed := newOutpostStub(t, func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 64; i++ {
			_, _ = io.WriteString(w, strings.Repeat("x", 64<<10))
			flusher.Flush()
		}
	})
	c = newTestClient(t, Config{ServerURL: streamed.URL + "/api/v2", MaxResponseBytes: 256 << 10})
	_, err = c.ListEvents(context.Background(), "t", ListEventsQuery{})
	assert.ErrorContains(t, err, "too large")
}

func TestClientTimeoutAndCancel(t *testing.T) {
	release := make(chan struct{})
	s := newOutpostStub(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })

	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2", Timeout: 100 * time.Millisecond})
	start := time.Now()
	_, err := c.ListEvents(context.Background(), "t", ListEventsQuery{})
	var reqErr *RequestError
	require.ErrorAs(t, err, &reqErr)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.Less(t, time.Since(start), 2*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = newTestClient(t, Config{ServerURL: s.URL + "/api/v2"}).ListEvents(ctx, "t", ListEventsQuery{})
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestClientAPIKeySourceError(t *testing.T) {
	s := newOutpostStub(t, jsonAnswer(200, `{}`))
	c := newTestClient(t, Config{
		ServerURL:    s.URL + "/api/v2",
		APIKeySource: func(context.Context) (string, error) { return "", errors.New("vault down") },
	})
	_, err := c.ListEvents(context.Background(), "t", ListEventsQuery{})
	assert.ErrorContains(t, err, "vault down")
	assert.Equal(t, 0, s.count())
}

func TestMCPErrorFrom(t *testing.T) {
	want := &MCPError{StatusCode: 422, Kind: "callback_endpoint_error", Code: -32015, Message: "CallbackEndpointError", Data: json.RawMessage(`{"reason":"challenge_failed"}`)}

	got, ok := MCPErrorFrom(want)
	require.True(t, ok)
	assert.Same(t, want, got)

	got, ok = MCPErrorFrom(errors.Join(errors.New("wrapped"), want))
	require.True(t, ok)
	assert.Same(t, want, got)

	got, ok = MCPErrorFrom(providerErr{want})
	require.True(t, ok)
	assert.Same(t, want, got)

	// The shape of the generated apierrors.APIError.
	got, ok = MCPErrorFrom(&apiErrorShape{StatusCode: 422, Body: mcpErrorBody})
	require.True(t, ok)
	assert.Equal(t, want.Code, got.Code)
	assert.JSONEq(t, string(want.Data), string(got.Data))

	for _, err := range []error{
		errors.New("x"),
		&apiErrorShape{StatusCode: 500, Body: mcpErrorBody},
		&apiErrorShape{StatusCode: 422, Body: "{"},
		&apiErrorShape{StatusCode: 422, Body: strings.Repeat(" ", 70<<10) + mcpErrorBody},
		&apiErrorShape{StatusCode: 422, Body: `{"mcp_error":{"code":-1}}`},
		&unexportedShape{statusCode: 422, body: mcpErrorBody},
		providerErr{nil},
		(*apiErrorShape)(nil),
	} {
		_, ok := MCPErrorFrom(err)
		assert.False(t, ok, "%#v", err)
	}
}

type providerErr struct{ m *MCPError }

func (e providerErr) Error() string              { return "provider" }
func (e providerErr) OutpostMCPError() *MCPError { return e.m }

type apiErrorShape struct {
	Message    string
	StatusCode int
	Body       string
}

func (e *apiErrorShape) Error() string { return e.Message }

type unexportedShape struct {
	statusCode int
	body       string
}

func (e *unexportedShape) Error() string { return "unexported" }

func TestCodeProfile(t *testing.T) {
	codes, ok := CodeProfile("sep-3415")
	assert.True(t, ok)
	assert.Equal(t, CodesSEP3415, codes)
	codes, ok = CodeProfile("sketch")
	assert.True(t, ok)
	assert.Equal(t, CodesSketch, codes)
	_, ok = CodeProfile("nope")
	assert.False(t, ok)
}

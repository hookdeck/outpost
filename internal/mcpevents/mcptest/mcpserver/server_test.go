package mcpserver_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type upstreamCall struct {
	Method, Path, RawPath, Query, Auth string
	Body                               map[string]any
}

type fakeOutpost struct {
	*httptest.Server
	mu     sync.Mutex
	calls  []upstreamCall
	status int
	reply  string
}

func newFakeOutpost(t *testing.T) *fakeOutpost {
	f := &fakeOutpost{status: http.StatusOK, reply: `{"ok":true}`}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := upstreamCall{Method: r.Method, Path: r.URL.Path, RawPath: r.URL.EscapedPath(), Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization")}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &c.Body)
		}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		status, reply := f.status, f.reply
		f.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeOutpost) last(t *testing.T) upstreamCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.calls)
	return f.calls[len(f.calls)-1]
}

func (f *fakeOutpost) set(status int, reply string) {
	f.mu.Lock()
	f.status, f.reply = status, reply
	f.mu.Unlock()
}

type rpcReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func setup(t *testing.T) (*fakeOutpost, string) {
	up := newFakeOutpost(t)
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{
		OutpostURL: up.URL + "/api/v1",
		APIKey:     "key",
		Users: map[string]mcpserver.User{
			"alice": {Tenant: "t1"},
			"bob":   {Tenant: "t2", AllowedTopics: []string{"a", "b"}},
		},
	}))
	t.Cleanup(srv.Close)
	return up, srv.URL
}

func call(t *testing.T, url, token, body string, headers map[string]string) (int, rpcReply, string) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r rpcReply
	_ = json.Unmarshal(raw, &r)
	return resp.StatusCode, r, string(raw)
}

func TestInitializeAnswer(t *testing.T) {
	_, url := setup(t)
	status, r, _ := call(t, url, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
	require.Equal(t, 200, status)
	var res map[string]any
	require.NoError(t, json.Unmarshal(r.Result, &res))
	assert.Equal(t, "2026-07-28", res["protocolVersion"])
	caps := res["capabilities"].(map[string]any)
	assert.Contains(t, caps["extensions"], "io.modelcontextprotocol/events")
	assert.NotContains(t, caps, "events")
}

func TestDiscover(t *testing.T) {
	up, url := setup(t)
	status, r, _ := call(t, url, "", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`, nil)
	require.Equal(t, 200, status)
	assert.JSONEq(t, `{"resultType":"complete","ttlMs":0,"cacheScope":"public","supportedVersions":["2026-07-28"],
		"capabilities":{"extensions":{"io.modelcontextprotocol/events":{}}}}`, string(r.Result))
	up.mu.Lock()
	assert.Empty(t, up.calls)
	up.mu.Unlock()
}

func TestListForwardsCursor(t *testing.T) {
	up, url := setup(t)
	_, _, _ = call(t, url, "bob", `{"jsonrpc":"2.0","id":1,"method":"events/list","params":{"cursor":"c 1"}}`, nil)
	assert.Equal(t, "cursor=c+1&topics=a%2Cb", up.last(t).Query)
	_, _, _ = call(t, url, "alice", `{"jsonrpc":"2.0","id":2,"method":"events/list","params":{"cursor":"x"}}`, nil)
	assert.Equal(t, "cursor=x", up.last(t).Query)
}

func TestTokenIsTenant(t *testing.T) {
	up := newFakeOutpost(t)
	var lines []string
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{
		OutpostURL:    up.URL + "/api/v1",
		APIKey:        "key",
		TokenIsTenant: true,
		Logf:          func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) },
	}))
	t.Cleanup(srv.Close)

	h := map[string]string{"Mcp-Method": "events/subscribe", "Mcp-Name": "a"}
	_, _, _ = call(t, srv.URL, "anyone", `{"jsonrpc":"2.0","id":1,"method":"events/subscribe","params":{"name":"a"}}`, h)
	c := up.last(t)
	assert.Equal(t, "/api/v1/tenants/anyone/mcp/events/subscribe", c.Path)
	assert.Equal(t, "a", c.Body["name"])

	_, r, _ := call(t, srv.URL, "", `{"jsonrpc":"2.0","id":2,"method":"events/list"}`, nil)
	require.NotNil(t, r.Error)
	assert.Equal(t, -32024, r.Error.Code)

	assert.Equal(t, []string{
		"events/subscribe any... -> ok",
		"events/list - -> error -32024 Forbidden",
	}, lines)
}

func TestUnknownTokenForbidden(t *testing.T) {
	_, url := setup(t)
	for _, token := range []string{"", "mallory"} {
		status, r, _ := call(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"events/list"}`, nil)
		assert.Equal(t, 200, status)
		require.NotNil(t, r.Error)
		assert.Equal(t, -32024, r.Error.Code)
	}
}

func TestSubscribeHeaderMismatch(t *testing.T) {
	up, url := setup(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"events/subscribe","params":{"name":"a"}}`
	for _, h := range []map[string]string{
		nil,
		{"Mcp-Method": "events/subscribe"},
		{"Mcp-Method": "events/subscribe", "Mcp-Name": "other"},
		{"Mcp-Method": "events/unsubscribe", "Mcp-Name": "a"},
	} {
		status, r, _ := call(t, url, "alice", body, h)
		assert.Equal(t, 400, status)
		require.NotNil(t, r.Error)
		assert.Equal(t, -32020, r.Error.Code)
	}
	up.mu.Lock()
	assert.Empty(t, up.calls)
	up.mu.Unlock()
}

func TestListForwardsTopics(t *testing.T) {
	up, url := setup(t)
	_, _, _ = call(t, url, "alice", `{"jsonrpc":"2.0","id":1,"method":"events/list"}`, nil)
	c := up.last(t)
	assert.Equal(t, "/api/v1/mcp/events/list", c.Path)
	assert.Equal(t, "Bearer key", c.Auth)
	assert.Empty(t, c.Query)

	_, r, _ := call(t, url, "bob", `{"jsonrpc":"2.0","id":2,"method":"events/list"}`, nil)
	c = up.last(t)
	assert.Equal(t, "topics=a%2Cb", c.Query)
	assert.JSONEq(t, `{"ok":true}`, string(r.Result))
}

func TestSubscribeForwardsParams(t *testing.T) {
	up, url := setup(t)
	h := map[string]string{"Mcp-Method": "events/subscribe", "Mcp-Name": "a"}
	body := `{"jsonrpc":"2.0","id":1,"method":"events/subscribe","params":{"name":"a","delivery":{"url":"https://x"}}}`

	_, _, _ = call(t, url, "alice", body, h)
	c := up.last(t)
	assert.Equal(t, http.MethodPost, c.Method)
	assert.Equal(t, "/api/v1/tenants/t1/mcp/events/subscribe", c.Path)
	assert.Equal(t, "a", c.Body["name"])
	assert.Equal(t, map[string]any{"url": "https://x"}, c.Body["delivery"])
	assert.NotContains(t, c.Body, "allowed_topics")
	assert.NotContains(t, c.Body, "params")

	_, _, _ = call(t, url, "bob", body, h)
	c = up.last(t)
	assert.Equal(t, "/api/v1/tenants/t2/mcp/events/subscribe", c.Path)
	assert.NotContains(t, c.Body, "allowed_topics")

	h = map[string]string{"Mcp-Method": "events/unsubscribe", "Mcp-Name": "a"}
	_, _, _ = call(t, url, "bob", `{"jsonrpc":"2.0","id":1,"method":"events/unsubscribe","params":{"name":"a"}}`, h)
	c = up.last(t)
	assert.Equal(t, "/api/v1/tenants/t2/mcp/events/unsubscribe", c.Path)
	assert.NotContains(t, c.Body, "allowed_topics")
}

func TestUpstreamErrors(t *testing.T) {
	up, url := setup(t)
	h := map[string]string{"Mcp-Method": "events/subscribe", "Mcp-Name": "a"}
	body := `{"jsonrpc":"2.0","id":7,"method":"events/subscribe","params":{"name":"a"}}`

	up.set(422, `{"mcp_error":{"code":-32027,"message":"CallbackEndpointError","data":{"reason":"challenge_failed"}}}`)
	_, _, raw := call(t, url, "alice", body, h)
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":7,"error":{"code":-32027,"message":"CallbackEndpointError","data":{"reason":"challenge_failed"}}}`, raw)

	up.set(500, `boom`)
	status, r, _ := call(t, url, "alice", body, h)
	assert.Equal(t, 200, status)
	require.NotNil(t, r.Error)
	assert.Equal(t, -32603, r.Error.Code)
}

func TestUnknownMethodAndNotification(t *testing.T) {
	_, url := setup(t)
	_, r, _ := call(t, url, "alice", `{"jsonrpc":"2.0","id":1,"method":"events/poll"}`, nil)
	require.NotNil(t, r.Error)
	assert.Equal(t, -32601, r.Error.Code)

	status, _, raw := call(t, url, "", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	assert.Equal(t, 202, status)
	assert.Empty(t, raw)
}

func TestLogMasksBearerToken(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{
		TokenIsTenant: true,
		Logf: func(format string, args ...any) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(format, args...))
			mu.Unlock()
		},
	}))
	defer srv.Close()
	call(t, srv.URL, "supersecrettoken", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`, nil)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, lines, 1)
	assert.NotContains(t, lines[0], "supersecrettoken")
	assert.Contains(t, lines[0], "supe...")
}

func TestExtraHeadersSentToOutpost(t *testing.T) {
	var got http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = io.WriteString(w, `{"events":[]}`)
	}))
	defer up.Close()
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{
		OutpostURL:    up.URL,
		APIKey:        "key",
		TokenIsTenant: true,
		Header:        http.Header{"X-Debug": {"1"}, "Authorization": {"Bearer other"}},
	}))
	defer srv.Close()
	call(t, srv.URL, "alice", `{"jsonrpc":"2.0","id":1,"method":"events/list","params":{}}`, nil)
	assert.Equal(t, "1", got.Get("X-Debug"))
	assert.Equal(t, "Bearer other", got.Get("Authorization"))
}

func TestOutpostFailureLogged(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream down")
	}))
	defer up.Close()
	var mu sync.Mutex
	var lines []string
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{
		OutpostURL:    up.URL,
		TokenIsTenant: true,
		Logf: func(format string, args ...any) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(format, args...))
			mu.Unlock()
		},
	}))
	defer srv.Close()
	_, r, _ := call(t, srv.URL, "alice", `{"jsonrpc":"2.0","id":1,"method":"events/list","params":{}}`, nil)
	require.NotNil(t, r.Error)
	assert.Equal(t, -32603, r.Error.Code)
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "http 502: upstream down")
}

func TestTokenTenantIsPathEscaped(t *testing.T) {
	up := newFakeOutpost(t)
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{OutpostURL: up.URL + "/api/v1", APIKey: "key", TokenIsTenant: true}))
	t.Cleanup(srv.Close)
	h := map[string]string{"Mcp-Method": "events/subscribe", "Mcp-Name": "a"}
	_, _, _ = call(t, srv.URL, "a/b c", `{"jsonrpc":"2.0","id":1,"method":"events/subscribe","params":{"name":"a"}}`, h)
	assert.Equal(t, "/api/v1/tenants/a%2Fb%20c/mcp/events/subscribe", up.last(t).RawPath)
}

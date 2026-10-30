package mcpclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seen struct {
	Header http.Header
	Body   map[string]any
}

func newServer(t *testing.T, handle func(w http.ResponseWriter, method string, id any, params map[string]any)) (*httptest.Server, func() []seen) {
	t.Helper()
	var mu sync.Mutex
	var log []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		mu.Lock()
		log = append(log, seen{Header: r.Header.Clone(), Body: body})
		mu.Unlock()
		method, _ := body["method"].(string)
		params, _ := body["params"].(map[string]any)
		handle(w, method, body["id"], params)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seen {
		mu.Lock()
		defer mu.Unlock()
		return append([]seen(nil), log...)
	}
}

func reply(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func TestInitializeHandshake(t *testing.T) {
	srv, log := newServer(t, func(w http.ResponseWriter, method string, id any, _ map[string]any) {
		switch method {
		case "initialize":
			w.Header().Set(HeaderSessionID, "sess-1")
			reply(w, id, map[string]any{
				"protocolVersion": DefaultProtocolVersion,
				"capabilities":    map[string]any{"events": map[string]any{}, "extensions": map[string]any{ExtensionID: map[string]any{}}},
			})
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})
	c := New(srv.URL, "alice")
	res, _, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.Capabilities.Extensions, ExtensionID)
	assert.NotNil(t, res.Capabilities.Events)
	assert.Equal(t, "sess-1", c.SessionID())

	reqs := log()
	require.Len(t, reqs, 2)
	assert.Equal(t, "initialize", reqs[0].Header.Get(HeaderMethod))
	assert.Equal(t, "Bearer alice", reqs[0].Header.Get("Authorization"))
	assert.Empty(t, reqs[0].Header.Get(HeaderSessionID))
	assert.Equal(t, "notifications/initialized", reqs[1].Body["method"])
	assert.Equal(t, "sess-1", reqs[1].Header.Get(HeaderSessionID))
	assert.Equal(t, DefaultProtocolVersion, reqs[1].Header.Get(HeaderProtocolVersion))
	assert.NotContains(t, reqs[1].Body, "id")
}

func TestSubscribeHeadersAndBody(t *testing.T) {
	srv, log := newServer(t, func(w http.ResponseWriter, _ string, id any, _ map[string]any) {
		reply(w, id, map[string]any{"id": "sub_1", "refreshBefore": "2030-01-01T00:00:00Z", "cursor": nil, "truncated": false})
	})
	c := New(srv.URL, "alice")
	p := SubscribeParams{Name: "user.created", Arguments: map[string]any{"status": "paid"}, URL: "https://x.example/", Secret: "whsec_a", TTLMs: TTLNull}

	resp, err := c.Subscribe(context.Background(), p)
	require.NoError(t, err)
	var got SubscribeResult
	require.NoError(t, resp.Decode(&got))
	assert.Equal(t, "sub_1", got.ID)
	assert.Nil(t, got.Cursor)

	_, err = c.Subscribe(context.Background(), p, NameHeader("other"))
	require.NoError(t, err)
	_, err = c.Subscribe(context.Background(), p, OmitNameHeader())
	require.NoError(t, err)
	_, err = c.EventsList(context.Background())
	require.NoError(t, err)

	reqs := log()
	require.Len(t, reqs, 4)
	assert.Equal(t, "events/subscribe", reqs[0].Header.Get(HeaderMethod))
	assert.Equal(t, "user.created", reqs[0].Header.Get(HeaderName))
	assert.Equal(t, "other", reqs[1].Header.Get(HeaderName))
	assert.Empty(t, reqs[2].Header[http.CanonicalHeaderKey(HeaderName)])
	assert.Empty(t, reqs[3].Header[http.CanonicalHeaderKey(HeaderName)])

	params := reqs[0].Body["params"].(map[string]any)
	assert.Equal(t, "user.created", params["name"])
	assert.Equal(t, map[string]any{"status": "paid"}, params["arguments"])
	assert.Equal(t, map[string]any{"mode": "webhook", "url": "https://x.example/", "secret": "whsec_a"}, params["delivery"])
	assert.Contains(t, params, "ttlMs")
	assert.Nil(t, params["ttlMs"])
}

func TestSubscribeOmitsOptionalParams(t *testing.T) {
	b, err := json.Marshal(SubscribeParams{Name: "n", URL: "u", Secret: "s"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"n","delivery":{"mode":"webhook","url":"u","secret":"s"}}`, string(b))
	b, err = json.Marshal(UnsubscribeParams{Name: "n", URL: "u"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"n","delivery":{"url":"u"}}`, string(b))
}

func TestJSONRPCErrorAndHTTPError(t *testing.T) {
	status := http.StatusOK
	srv, _ := newServer(t, func(w http.ResponseWriter, _ string, id any, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{
			"code": -32023, "message": "NotFound", "data": map[string]any{"kind": "subscription"},
		}})
	})
	c := New(srv.URL, "alice")
	resp, err := c.Unsubscribe(context.Background(), UnsubscribeParams{Name: "n", URL: "u"})
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32023, resp.Error.Code)
	assert.Equal(t, "subscription", resp.Error.DataMap()["kind"])
	var out map[string]any
	assert.Error(t, resp.Decode(&out))

	status = http.StatusBadRequest
	resp, err = c.Subscribe(context.Background(), SubscribeParams{Name: "n"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.Status)
	assert.Equal(t, -32023, resp.Error.Code)
}

func TestEventStreamReply(t *testing.T) {
	srv, _ := newServer(t, func(w http.ResponseWriter, _ string, id any, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%v,\"result\":{\"events\":[{\"name\":\"a\"}]}}\n\n", id)
	})
	resp, err := New(srv.URL, "alice").EventsList(context.Background())
	require.NoError(t, err)
	var list ListResult
	require.NoError(t, resp.Decode(&list))
	require.Len(t, list.Events, 1)
	assert.Equal(t, "a", list.Events[0].Name)
}

func TestWithoutPrincipalSendsNoAuthorization(t *testing.T) {
	srv, log := newServer(t, func(w http.ResponseWriter, _ string, id any, _ map[string]any) {
		reply(w, id, map[string]any{"events": []any{}})
	})
	_, err := New(srv.URL, "alice", WithoutPrincipal()).EventsList(context.Background())
	require.NoError(t, err)
	assert.Empty(t, log()[0].Header.Get("Authorization"))
}

func TestNewSecret(t *testing.T) {
	a, b := NewSecret(), NewSecret()
	assert.NotEqual(t, a, b)
	raw, ok := strings.CutPrefix(a, "whsec_")
	require.True(t, ok)
	key, err := base64.StdEncoding.DecodeString(raw)
	require.NoError(t, err)
	assert.Len(t, key, 32)
}

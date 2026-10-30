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

func TestStatelessSendsMetaAndVersionOnEveryRequest(t *testing.T) {
	srv, log := newServer(t, func(w http.ResponseWriter, method string, id any, _ map[string]any) {
		reply(w, id, map[string]any{
			"resultType":        "complete",
			"supportedVersions": []string{DefaultProtocolVersion},
			"capabilities":      map[string]any{"extensions": map[string]any{ExtensionID: map[string]any{}}},
		})
	})
	c := New(srv.URL, "alice", Stateless(), WithClientInfoName("tester"))

	res, _, err := c.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "complete", res.ResultType)
	assert.Equal(t, []string{DefaultProtocolVersion}, res.SupportedVersions)
	assert.Contains(t, res.Capabilities.Extensions, ExtensionID)

	_, err = c.Subscribe(context.Background(), SubscribeParams{Name: "a", URL: "https://x", Secret: "s"})
	require.NoError(t, err)

	reqs := log()
	require.Len(t, reqs, 2)
	assert.Empty(t, c.SessionID())
	for _, r := range reqs {
		assert.Equal(t, DefaultProtocolVersion, r.Header.Get(HeaderProtocolVersion))
		assert.Empty(t, r.Header.Get(HeaderSessionID))
		meta := r.Body["params"].(map[string]any)["_meta"].(map[string]any)
		assert.Equal(t, DefaultProtocolVersion, meta[MetaProtocolVersion])
		assert.Equal(t, map[string]any{"name": "tester", "version": "0"}, meta[MetaClientInfo])
		caps := meta[MetaClientCapabilities].(map[string]any)
		assert.Contains(t, caps["extensions"], ExtensionID)
	}
	assert.Equal(t, "a", reqs[1].Body["params"].(map[string]any)["name"])
	assert.Equal(t, "a", reqs[1].Header.Get(HeaderName))
}

func TestStatefulRequestsHaveNoMeta(t *testing.T) {
	srv, log := newServer(t, func(w http.ResponseWriter, method string, id any, _ map[string]any) {
		reply(w, id, map[string]any{"resultType": "complete", "events": []any{}})
	})
	_, err := New(srv.URL, "alice").EventsList(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, log()[0].Body["params"], "_meta")
	assert.Empty(t, log()[0].Header.Get(HeaderProtocolVersion))
}

func TestEventsListPageCursor(t *testing.T) {
	srv, log := newServer(t, func(w http.ResponseWriter, method string, id any, _ map[string]any) {
		reply(w, id, map[string]any{"resultType": "complete", "events": []any{}})
	})
	c := New(srv.URL, "alice")
	_, err := c.EventsListPage(context.Background(), "")
	require.NoError(t, err)
	_, err = c.EventsListPage(context.Background(), "p2")
	require.NoError(t, err)
	reqs := log()
	assert.NotContains(t, reqs[0].Body["params"], "cursor")
	assert.Equal(t, "p2", reqs[1].Body["params"].(map[string]any)["cursor"])
}

func TestSubscribeParamsCursorAndMaxAge(t *testing.T) {
	marshal := func(p SubscribeParams) map[string]any {
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))
		return m
	}
	m := marshal(SubscribeParams{Name: "a"})
	assert.NotContains(t, m, "cursor")
	assert.NotContains(t, m, "maxAgeMs")

	m = marshal(SubscribeParams{Name: "a", Cursor: json.RawMessage("null"), MaxAgeMs: json.RawMessage("null")})
	assert.Contains(t, m, "cursor")
	assert.Nil(t, m["cursor"])
	assert.Contains(t, m, "maxAgeMs")
	assert.Nil(t, m["maxAgeMs"])

	m = marshal(SubscribeParams{Name: "a", Cursor: "c1", MaxAgeMs: 5000})
	assert.Equal(t, "c1", m["cursor"])
	assert.EqualValues(t, 5000, m["maxAgeMs"])
}

func TestDeliveryStatusDecodesAllFields(t *testing.T) {
	var res SubscribeResult
	require.NoError(t, json.Unmarshal([]byte(`{"resultType":"complete","id":"s","deliveryStatus":{
		"active":false,"lastDeliveryAt":"2026-02-19T15:45:00Z","lastError":"http_5xx",
		"failedSince":"2026-02-19T15:50:00Z","throttled":true,"retryAfterMs":3000}}`), &res))
	ds := res.DeliveryStatus
	require.NotNil(t, ds)
	assert.False(t, ds.Active)
	assert.Equal(t, "http_5xx", *ds.LastError)
	assert.Equal(t, "2026-02-19T15:50:00Z", *ds.FailedSince)
	assert.True(t, *ds.Throttled)
	assert.EqualValues(t, 3000, *ds.RetryAfterMs)
	assert.Contains(t, LastErrorCategories, *ds.LastError)
}

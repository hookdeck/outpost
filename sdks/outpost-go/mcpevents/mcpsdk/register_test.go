package mcpsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/sdks/outpost-go/mcpevents"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type apiCall struct {
	Method   string
	TenantID string
	Arg      any
}

type fakeAPI struct {
	mu     sync.Mutex
	calls  []apiCall
	answer func(method string) (json.RawMessage, error)
}

func (f *fakeAPI) respond(method, tenantID string, arg any) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, apiCall{method, tenantID, arg})
	f.mu.Unlock()
	if f.answer == nil {
		return json.RawMessage(fmt.Sprintf(`{"from":%q}`, method)), nil
	}
	return f.answer(method)
}

func (f *fakeAPI) ListEvents(_ context.Context, tenantID string, q mcpevents.ListEventsQuery) (json.RawMessage, error) {
	return f.respond("ListEvents", tenantID, q)
}

func (f *fakeAPI) Subscribe(_ context.Context, tenantID string, req mcpevents.SubscribeRequest) (json.RawMessage, error) {
	return f.respond("Subscribe", tenantID, req)
}

func (f *fakeAPI) Unsubscribe(_ context.Context, tenantID string, req mcpevents.UnsubscribeRequest) (json.RawMessage, error) {
	return f.respond("Unsubscribe", tenantID, req)
}

func (f *fakeAPI) recorded() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]apiCall(nil), f.calls...)
}

// principalFromToken reads the subject a bearer token verifier set.
func principalFromToken(_ context.Context, req mcp.Request) (string, error) {
	if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
		return extra.TokenInfo.UserID, nil
	}
	return "", nil
}

// serve starts an MCP server with the events methods behind a bearer token
// verifier mapping each token to the principal of the same name. Only a
// stateless handler serves the 2026-07-28 revision.
func serve(t *testing.T, api mcpevents.API, hopts mcpevents.Options, ropts Options, withAuth, stateless bool) string {
	t.Helper()
	if hopts.ResolveTenant == nil {
		hopts.ResolveTenant = func(_ context.Context, principal string) (string, error) { return "store_" + principal, nil }
	}
	if ropts.ResolvePrincipal == nil {
		ropts.ResolvePrincipal = principalFromToken
	}
	h, err := mcpevents.NewHandlers(api, hopts)
	require.NoError(t, err)
	server := mcp.NewServer(&mcp.Implementation{Name: "outpost-test", Version: "1.0.0"}, nil)
	require.NoError(t, Register(server, h, ropts))

	var handler http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true, Stateless: stateless})
	if withAuth {
		verify := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: token, Expiration: time.Now().Add(time.Hour)}, nil
		}
		handler = auth.RequireBearerToken(verify, nil)(handler)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts.URL
}

// recorder adds the bearer token and keeps every response body.
type recorder struct {
	token  string
	mu     sync.Mutex
	bodies []string
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	// JSON responses only: the standalone SSE stream of a 2025-era session
	// never ends.
	if err != nil || resp.Body == nil || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.bodies = append(r.bodies, string(body))
	r.mu.Unlock()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// capabilities returns the capabilities of the first handshake result seen.
func (r *recorder) capabilities(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, body := range r.bodies {
		var msg struct {
			Result struct {
				Capabilities map[string]json.RawMessage `json:"capabilities"`
				ResultType   string                     `json:"resultType"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(body), &msg) == nil && msg.Result.Capabilities != nil {
			return msg.Result.Capabilities
		}
	}
	t.Fatal("no handshake result recorded")
	return nil
}

func (r *recorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies[len(r.bodies)-1]
}

type eventParams struct {
	mcp.ParamsBase
	Name     string         `json:"name,omitempty"`
	Cursor   string         `json:"cursor,omitempty"`
	Delivery map[string]any `json:"delivery,omitempty"`
}

type eventResult struct {
	mcp.ResultBase
	Events     []map[string]any `json:"events,omitempty"`
	NextCursor string           `json:"nextCursor,omitempty"`
	ID         string           `json:"id,omitempty"`
	From       string           `json:"from,omitempty"`
}

func connect(t *testing.T, url string, rec *recorder, protocolVersion string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	for _, method := range []string{mcpevents.MethodList, mcpevents.MethodSubscribe, mcpevents.MethodUnsubscribe} {
		require.NoError(t, mcp.AddSendingCustomMethod[*eventParams, *eventResult](client, method))
	}
	transport := &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: rec}}
	cs, err := client.Connect(context.Background(), transport, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(cs *mcp.ClientSession, method string, params *eventParams) (*eventResult, error) {
	return mcp.CallCustomMethod[*eventParams, *eventResult](context.Background(), cs, method, params)
}

func TestRegisterServesBothProtocolEras(t *testing.T) {
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			api := &fakeAPI{answer: func(method string) (json.RawMessage, error) {
				if method == "ListEvents" {
					return json.RawMessage(`{"events":[{"name":"order.created"}],"nextCursor":"n"}`), nil
				}
				return json.RawMessage(`{"id":"sub_1","refreshBefore":null,"cursor":null,"truncated":false}`), nil
			}}
			modern := version == "2026-07-28"
			url := serve(t, api, mcpevents.Options{AllowedTopics: func(context.Context, string) ([]string, error) {
				return []string{"order.created"}, nil
			}}, Options{}, true, modern)
			rec := &recorder{token: "user_1"}
			cs := connect(t, url, rec, version)
			require.Equal(t, version, cs.InitializeResult().ProtocolVersion)

			caps := rec.capabilities(t)
			assert.JSONEq(t, `{}`, string(caps["events"]))
			assert.JSONEq(t, `{"io.modelcontextprotocol/events":{}}`, string(caps["extensions"]))

			listed, err := call(cs, mcpevents.MethodList, &eventParams{Cursor: "c"})
			require.NoError(t, err)
			assert.Equal(t, "n", listed.NextCursor)
			assert.Equal(t, "order.created", listed.Events[0]["name"])
			if modern {
				assert.Contains(t, rec.last(), `"resultType":"complete"`, "the revision requires resultType")
			} else {
				assert.NotContains(t, rec.last(), `"resultType"`)
			}

			delivery := map[string]any{"mode": "webhook", "url": "https://x", "secret": "whsec_a"}
			subscribed, err := call(cs, mcpevents.MethodSubscribe, &eventParams{Name: "order.created", Delivery: delivery})
			require.NoError(t, err)
			assert.Equal(t, "sub_1", subscribed.ID)
			_, err = call(cs, mcpevents.MethodUnsubscribe, &eventParams{Name: "order.created", Delivery: delivery})
			require.NoError(t, err)

			calls := api.recorded()
			require.Len(t, calls, 3)
			assert.Equal(t, "store_user_1", calls[0].TenantID)
			assert.Equal(t, mcpevents.ListEventsQuery{Cursor: "c", Topics: []string{"order.created"}}, calls[0].Arg)
			sub := calls[1].Arg.(mcpevents.SubscribeRequest)
			assert.Equal(t, "user_1", sub.Principal)
			assert.Equal(t, []string{"order.created"}, sub.AllowedTopics)
			var sent map[string]any
			require.NoError(t, json.Unmarshal(sub.Params, &sent))
			assert.Equal(t, "order.created", sent["name"])
			assert.Equal(t, delivery, sent["delivery"])
			assert.Equal(t, "store_user_1", calls[2].TenantID)
		})
	}
}

func TestRegisterErrorsReachTheClient(t *testing.T) {
	api := &fakeAPI{answer: func(method string) (json.RawMessage, error) {
		if method == "Subscribe" {
			return nil, &mcpevents.MCPError{StatusCode: 422, Kind: "resource_exhausted", Code: -32013, Message: "ResourceExhausted", Data: json.RawMessage(`{"limit":"subscriptions","max":100}`)}
		}
		return nil, errors.New("dial tcp 10.1.2.3:3333: connection refused")
	}}
	url := serve(t, api, mcpevents.Options{AllowedTopics: func(context.Context, string) ([]string, error) {
		return []string{"a"}, nil
	}}, Options{}, true, true)
	cs := connect(t, url, &recorder{token: "u"}, "")

	_, err := call(cs, mcpevents.MethodSubscribe, &eventParams{Name: "a"})
	wire := requireWireError(t, err, -32013)
	assert.Equal(t, "ResourceExhausted", wire.Message)
	assert.JSONEq(t, `{"limit":"subscriptions","max":100}`, string(wire.Data))

	_, err = call(cs, mcpevents.MethodSubscribe, &eventParams{Name: "b"})
	wire = requireWireError(t, err, -32011)
	assert.JSONEq(t, `{"kind":"event"}`, string(wire.Data))

	_, err = call(cs, mcpevents.MethodUnsubscribe, &eventParams{})
	wire = requireWireError(t, err, -32603)
	assert.Equal(t, "Internal error", wire.Message)
	assert.NotContains(t, err.Error(), "10.1.2.3")
}

func TestRegisterMissingPrincipalIsForbidden(t *testing.T) {
	api := &fakeAPI{}
	url := serve(t, api, mcpevents.Options{Codes: mcpevents.CodesSEP3415}, Options{}, false, true)
	cs := connect(t, url, &recorder{}, "")
	for _, method := range []string{mcpevents.MethodList, mcpevents.MethodSubscribe, mcpevents.MethodUnsubscribe} {
		_, err := call(cs, method, &eventParams{})
		requireWireError(t, err, -32024)
	}
	assert.Empty(t, api.recorded())
}

func TestRegisterPrincipalResolverErrors(t *testing.T) {
	var reported []string
	api := &fakeAPI{}
	url := serve(t, api, mcpevents.Options{
		OnError: func(_ context.Context, method string, err error) {
			reported = append(reported, method+": "+err.Error())
		},
	}, Options{ResolvePrincipal: func(_ context.Context, req mcp.Request) (string, error) {
		if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil && extra.TokenInfo.UserID == "suspended" {
			return "", &mcpevents.RPCError{Code: -32012, Message: "Forbidden", Data: json.RawMessage(`{"reason":"suspended"}`)}
		}
		return "", errors.New("token store down")
	}}, true, true)

	_, err := call(connect(t, url, &recorder{token: "u"}, ""), mcpevents.MethodList, &eventParams{})
	requireWireError(t, err, -32603)
	assert.Equal(t, []string{"events/list: token store down"}, reported)

	_, err = call(connect(t, url, &recorder{token: "suspended"}, ""), mcpevents.MethodList, &eventParams{})
	wire := requireWireError(t, err, -32012)
	assert.JSONEq(t, `{"reason":"suspended"}`, string(wire.Data))
	assert.Empty(t, api.recorded())
}

func requireWireError(t *testing.T, err error, code int64) *jsonrpc.Error {
	t.Helper()
	var wire *jsonrpc.Error
	require.ErrorAs(t, err, &wire)
	assert.Equal(t, code, wire.Code)
	return wire
}

func TestRegisterValidation(t *testing.T) {
	h, err := mcpevents.NewHandlers(&fakeAPI{}, mcpevents.Options{ResolveTenant: func(context.Context, string) (string, error) { return "t", nil }})
	require.NoError(t, err)
	server := mcp.NewServer(&mcp.Implementation{Name: "s", Version: "1"}, nil)
	assert.Error(t, Register(nil, h, Options{ResolvePrincipal: principalFromToken}))
	assert.Error(t, Register(server, nil, Options{ResolvePrincipal: principalFromToken}))
	assert.Error(t, Register(server, h, Options{}))
}

func TestAddMembers(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`{}`, `{"resultType":"complete","_meta":{"k":1}}`},
		{` { } `, `{ "resultType":"complete","_meta":{"k":1}}`},
		{`{"a":[1,{"b":"}"}]}`, `{"a":[1,{"b":"}"}],"resultType":"complete","_meta":{"k":1}}`},
		{`{"resultType":"complete","a":1}`, `{"resultType":"complete","a":1,"_meta":{"k":1}}`},
		{`{"_meta":{"x":2}}`, `{"_meta":{"x":2},"resultType":"complete"}`},
	}
	members := []member{{"resultType", json.RawMessage(`"complete"`)}, {"_meta", json.RawMessage(`{"k":1}`)}}
	for _, tt := range tests {
		got, err := addMembers(json.RawMessage(tt.in), members)
		require.NoError(t, err, tt.in)
		assert.Equal(t, tt.want, string(got), tt.in)
		assert.True(t, json.Valid(got))
	}
	for _, bad := range []string{``, `[]`, `"x"`, `{`} {
		_, err := addMembers(json.RawMessage(bad), members)
		assert.Error(t, err, bad)
	}
	got, err := addMembers(json.RawMessage(` {"a":1} `), nil)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(got))
}

func TestCapabilitiesResultKeepsExistingEntries(t *testing.T) {
	inner := &mcp.InitializeResult{
		ProtocolVersion: "2025-11-25",
		ServerInfo:      &mcp.Implementation{Name: "s", Version: "1"},
		Capabilities:    &mcp.ServerCapabilities{Extensions: map[string]any{"io.example/other": map[string]any{"x": true}}},
	}
	data, err := json.Marshal(&capabilitiesResult{inner: inner})
	require.NoError(t, err)
	var got struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
		ResultType   *string                    `json:"resultType"`
	}
	require.NoError(t, json.Unmarshal(data, &got))
	assert.JSONEq(t, `{}`, string(got.Capabilities["events"]))
	assert.JSONEq(t, `{"io.example/other":{"x":true},"io.modelcontextprotocol/events":{}}`, string(got.Capabilities["extensions"]))
	assert.Nil(t, got.ResultType, "initialize is a 2025-era result")
	assert.True(t, strings.Contains(string(data), `"serverInfo"`))
}

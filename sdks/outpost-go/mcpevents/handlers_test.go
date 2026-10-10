package mcpevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type apiCall struct {
	Method   string
	TenantID string
	Arg      any
}

// fakeAPI records calls and answers with answer.
type fakeAPI struct {
	calls  []apiCall
	answer func(method string) (json.RawMessage, error)
}

func (f *fakeAPI) respond(method, tenantID string, arg any) (json.RawMessage, error) {
	f.calls = append(f.calls, apiCall{method, tenantID, arg})
	if f.answer == nil {
		return json.RawMessage(fmt.Sprintf(`{"from":%q}`, method)), nil
	}
	return f.answer(method)
}

func (f *fakeAPI) ListEvents(_ context.Context, tenantID string, q ListEventsQuery) (json.RawMessage, error) {
	return f.respond("ListEvents", tenantID, q)
}

func (f *fakeAPI) Subscribe(_ context.Context, tenantID string, req SubscribeRequest) (json.RawMessage, error) {
	return f.respond("Subscribe", tenantID, req)
}

func (f *fakeAPI) Unsubscribe(_ context.Context, tenantID string, req UnsubscribeRequest) (json.RawMessage, error) {
	return f.respond("Unsubscribe", tenantID, req)
}

func tenantOf(_ context.Context, principal string) (string, error) {
	return "tenant_of_" + principal, nil
}

func newTestHandlers(t *testing.T, api API, opts Options) *Handlers {
	t.Helper()
	if opts.ResolveTenant == nil {
		opts.ResolveTenant = tenantOf
	}
	h, err := NewHandlers(api, opts)
	require.NoError(t, err)
	return h
}

func requireRPC(t *testing.T, err error, code int64, message string, data string) {
	t.Helper()
	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, code, rpcErr.Code)
	assert.Equal(t, message, rpcErr.Message)
	if data == "" {
		assert.Empty(t, rpcErr.Data)
	} else {
		assert.JSONEq(t, data, string(rpcErr.Data))
	}
}

func allow(topics ...string) func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) { return topics, nil }
}

func TestNewHandlersRequiresOptions(t *testing.T) {
	_, err := NewHandlers(nil, Options{ResolveTenant: tenantOf})
	assert.Error(t, err)
	_, err = NewHandlers(&fakeAPI{}, Options{})
	assert.Error(t, err)
	h := newTestHandlers(t, &fakeAPI{}, Options{})
	assert.Equal(t, CodesSketch, h.Codes())
}

func TestHandlersForwardEachMethod(t *testing.T) {
	api := &fakeAPI{}
	h := newTestHandlers(t, api, Options{})
	ctx := context.Background()
	params := json.RawMessage(`{"name":"order.created","arguments":{},"delivery":{"url":"https://x"}}`)

	got, err := h.HandleList(ctx, "u1", json.RawMessage(`{"cursor":"c1"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"from":"ListEvents"}`, string(got))
	_, err = h.HandleSubscribe(ctx, "u1", params)
	require.NoError(t, err)
	_, err = h.HandleUnsubscribe(ctx, "u1", params)
	require.NoError(t, err)

	assert.Equal(t, []apiCall{
		{"ListEvents", "tenant_of_u1", ListEventsQuery{Cursor: "c1"}},
		{"Subscribe", "tenant_of_u1", SubscribeRequest{Principal: "u1", Params: params}},
		{"Unsubscribe", "tenant_of_u1", UnsubscribeRequest{Principal: "u1", Params: params}},
	}, api.calls)
}

func TestHandlersDispatchByName(t *testing.T) {
	api := &fakeAPI{}
	h := newTestHandlers(t, api, Options{})
	for _, method := range []string{MethodList, MethodSubscribe, MethodUnsubscribe} {
		_, err := h.Handle(context.Background(), method, "u", nil)
		require.NoError(t, err)
	}
	_, err := h.Handle(context.Background(), "events/poll", "u", nil)
	requireRPC(t, err, -32601, "Method not found", "")
	assert.Len(t, api.calls, 3)
}

func TestHandlersMissingPrincipalIsForbidden(t *testing.T) {
	api := &fakeAPI{}
	resolved := 0
	h := newTestHandlers(t, api, Options{ResolveTenant: func(context.Context, string) (string, error) { resolved++; return "t", nil }})
	for _, method := range []string{MethodList, MethodSubscribe, MethodUnsubscribe} {
		_, err := h.Handle(context.Background(), method, "", json.RawMessage(`{}`))
		requireRPC(t, err, -32012, "Forbidden", "")
	}
	assert.Empty(t, api.calls)
	assert.Zero(t, resolved)

	h = newTestHandlers(t, api, Options{Codes: CodesSEP3415})
	_, err := h.HandleSubscribe(context.Background(), "", nil)
	requireRPC(t, err, -32024, "Forbidden", "")
}

func TestHandlersPrincipalWithoutTenantIsForbidden(t *testing.T) {
	api := &fakeAPI{}
	type ctxKey struct{}
	var seen []string
	h := newTestHandlers(t, api, Options{ResolveTenant: func(ctx context.Context, principal string) (string, error) {
		seen = append(seen, principal+"/"+ctx.Value(ctxKey{}).(string))
		return "", nil
	}})
	_, err := h.HandleList(context.WithValue(context.Background(), ctxKey{}, "req1"), "u", nil)
	requireRPC(t, err, -32012, "Forbidden", "")
	assert.Equal(t, []string{"u/req1"}, seen)
	assert.Empty(t, api.calls)
}

func TestHandlersParams(t *testing.T) {
	api := &fakeAPI{}
	h := newTestHandlers(t, api, Options{})
	for _, params := range []string{`[]`, `"x"`, `1`, `true`, `{`, `{"a":1}x`} {
		_, err := h.HandleSubscribe(context.Background(), "u", json.RawMessage(params))
		requireRPC(t, err, -32602, "InvalidParams", `{"field":"params","reason":"invalid"}`)
	}
	for _, params := range []string{"", "null", "  "} {
		_, err := h.HandleUnsubscribe(context.Background(), "u", json.RawMessage(params))
		require.NoError(t, err)
	}
	require.Len(t, api.calls, 3)
	assert.Equal(t, UnsubscribeRequest{Principal: "u", Params: json.RawMessage(`{}`)}, api.calls[0].Arg)
}

func TestHandlersCursor(t *testing.T) {
	api := &fakeAPI{}
	h := newTestHandlers(t, api, Options{})
	for _, cursor := range []string{`1`, `{}`, `[]`, `true`} {
		_, err := h.HandleList(context.Background(), "u", json.RawMessage(`{"cursor":`+cursor+`}`))
		requireRPC(t, err, -32602, "InvalidParams", `{"field":"cursor","reason":"invalid"}`)
	}
	_, err := h.HandleList(context.Background(), "u", json.RawMessage(`{"cursor":null}`))
	require.NoError(t, err)
	assert.Equal(t, []apiCall{{"ListEvents", "tenant_of_u", ListEventsQuery{}}}, api.calls)
}

func TestHandlersAllowedTopicsNarrowList(t *testing.T) {
	api := &fakeAPI{}
	var topics []string
	h := newTestHandlers(t, api, Options{AllowedTopics: func(context.Context, string) ([]string, error) { return topics, nil }})

	topics = []string{"order.created", "order.paid"}
	_, err := h.HandleList(context.Background(), "u", nil)
	require.NoError(t, err)
	assert.Equal(t, ListEventsQuery{Topics: []string{"order.created", "order.paid"}}, api.calls[0].Arg)

	// Empty, nil, and entries that can't be topic names all answer locally.
	for _, list := range [][]string{{}, nil, {"", "a,b"}} {
		topics = list
		got, err := h.HandleList(context.Background(), "u", json.RawMessage(`{"cursor":"c"}`))
		require.NoError(t, err)
		assert.Equal(t, `{"events":[]}`, string(got))
	}
	assert.Len(t, api.calls, 1)
}

func TestHandlersAllowedTopicsGuardSubscribe(t *testing.T) {
	api := &fakeAPI{}
	h := newTestHandlers(t, api, Options{AllowedTopics: allow("order.created")})

	_, err := h.HandleSubscribe(context.Background(), "u", json.RawMessage(`{"name":"order.refunded"}`))
	requireRPC(t, err, -32011, "NotFound", `{"kind":"event"}`)
	assert.Empty(t, api.calls)

	_, err = h.HandleSubscribe(context.Background(), "u", json.RawMessage(`{"name":"order.created"}`))
	require.NoError(t, err)
	assert.Equal(t, SubscribeRequest{Principal: "u", Params: json.RawMessage(`{"name":"order.created"}`), AllowedTopics: []string{"order.created"}}, api.calls[0].Arg)

	h = newTestHandlers(t, api, Options{AllowedTopics: allow()})
	_, err = h.HandleSubscribe(context.Background(), "u", json.RawMessage(`{"name":"order.created"}`))
	requireRPC(t, err, -32011, "NotFound", `{"kind":"event"}`)
	assert.Len(t, api.calls, 1)
}

func TestHandlersCaseVariantNameCannotSmuggleATopic(t *testing.T) {
	// encoding/json, Outpost's included, matches member names
	// case-insensitively; a later "Name" would win over "name".
	api := &fakeAPI{}
	h := newTestHandlers(t, api, Options{AllowedTopics: allow("allowed")})
	for _, params := range []string{
		`{"name":"allowed","Name":"secret.topic"}`,
		`{"NAME":"secret.topic"}`,
		`{"nAmE":"secret.topic","name":"allowed"}`,
		`{"name":"secret.topic","name":"allowed","Name":"secret.topic"}`,
	} {
		_, err := h.HandleSubscribe(context.Background(), "u", json.RawMessage(params))
		requireRPC(t, err, -32011, "NotFound", `{"kind":"event"}`)
	}
	assert.Empty(t, api.calls)

	var outpostView struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"name":"allowed","Name":"secret.topic"}`), &outpostView))
	assert.Equal(t, "secret.topic", outpostView.Name, "why the check folds case")
}

func TestHandlersNonStringNameIsLeftToOutpost(t *testing.T) {
	api := &fakeAPI{}
	h := newTestHandlers(t, api, Options{AllowedTopics: allow("a")})
	_, err := h.HandleSubscribe(context.Background(), "u", json.RawMessage(`{"name":["a"]}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, api.calls[0].Arg.(SubscribeRequest).AllowedTopics)
}

func TestHandlersUnsubscribeIsNeverFiltered(t *testing.T) {
	api := &fakeAPI{answer: func(string) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }}
	asked := 0
	h := newTestHandlers(t, api, Options{AllowedTopics: func(context.Context, string) ([]string, error) { asked++; return nil, nil }})
	got, err := h.HandleUnsubscribe(context.Background(), "u", json.RawMessage(`{"name":"anything"}`))
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(got))
	assert.Len(t, api.calls, 1)
	assert.Zero(t, asked)
}

func TestHandlersRethrowMCPErrors(t *testing.T) {
	mcpErr := &MCPError{StatusCode: 422, Kind: "callback_endpoint_error", Code: -32015, Message: "CallbackEndpointError", Data: json.RawMessage(`{"reason":"challenge_failed"}`)}
	for _, failure := range []error{
		mcpErr,
		fmt.Errorf("wrapped: %w", mcpErr),
		providerErr{mcpErr},
		&apiErrorShape{Message: "API error occurred", StatusCode: 422, Body: mcpErrorBody},
	} {
		api := &fakeAPI{answer: func(string) (json.RawMessage, error) { return nil, failure }}
		h := newTestHandlers(t, api, Options{Codes: CodesSEP3415})
		_, err := h.HandleSubscribe(context.Background(), "u", nil)
		// Outpost's own code wins over the local profile.
		requireRPC(t, err, -32015, "CallbackEndpointError", `{"reason":"challenge_failed"}`)
	}
}

func TestHandlersOtherFailuresAreInternal(t *testing.T) {
	secretBody := `{"message":"boom","data":["whsec_supersecret"]}`
	for _, failure := range []error{
		&RequestError{msg: "outpost: responded with status 500", StatusCode: 500, Body: secretBody},
		errors.New("dial tcp 10.0.0.1:3333: connect: connection refused"),
		&apiErrorShape{Message: "bad", StatusCode: http.StatusBadRequest, Body: secretBody},
	} {
		var reported []string
		api := &fakeAPI{answer: func(string) (json.RawMessage, error) { return nil, failure }}
		h := newTestHandlers(t, api, Options{OnError: func(_ context.Context, method string, err error) {
			reported = append(reported, method+": "+err.Error())
			panic("a broken logger")
		}})
		_, err := h.HandleSubscribe(context.Background(), "u", nil)
		requireRPC(t, err, -32603, "Internal error", "")
		assert.NotContains(t, err.Error(), "whsec_")
		assert.NotContains(t, err.Error(), "10.0.0.1")
		assert.Equal(t, []string{MethodSubscribe + ": " + failure.Error()}, reported)
	}
}

func TestHandlersResultMustBeAnObject(t *testing.T) {
	for _, result := range []string{"", "null", "[]", `"x"`, "1"} {
		api := &fakeAPI{answer: func(string) (json.RawMessage, error) { return json.RawMessage(result), nil }}
		h := newTestHandlers(t, api, Options{})
		_, err := h.HandleList(context.Background(), "u", nil)
		requireRPC(t, err, -32603, "Internal error", "")
	}
}

func TestHandlersResolverErrors(t *testing.T) {
	h := newTestHandlers(t, &fakeAPI{}, Options{ResolveTenant: func(_ context.Context, principal string) (string, error) {
		if principal == "custom" {
			return "", fmt.Errorf("denied: %w", &RPCError{Code: -32012, Message: "Forbidden", Data: json.RawMessage(`{"reason":"suspended"}`)})
		}
		return "", errors.New("db down")
	}})
	_, err := h.HandleList(context.Background(), "u", nil)
	requireRPC(t, err, -32603, "Internal error", "")
	_, err = h.HandleList(context.Background(), "custom", nil)
	requireRPC(t, err, -32012, "Forbidden", `{"reason":"suspended"}`)

	h = newTestHandlers(t, &fakeAPI{}, Options{AllowedTopics: func(context.Context, string) ([]string, error) {
		return nil, errors.New("acl down")
	}})
	_, err = h.HandleList(context.Background(), "u", nil)
	requireRPC(t, err, -32603, "Internal error", "")
}

func TestHandlersEndToEndWithClient(t *testing.T) {
	s := newOutpostStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			jsonAnswer(422, mcpErrorBody)(w, r)
			return
		}
		jsonAnswer(200, `{"events":[{"name":"a"}]}`)(w, r)
	})
	c := newTestClient(t, Config{ServerURL: s.URL + "/api/v2", APIKey: "k"})
	h := newTestHandlers(t, c, Options{AllowedTopics: allow("a")})

	got, err := h.HandleList(context.Background(), "u", nil)
	require.NoError(t, err)
	assert.Equal(t, `{"events":[{"name":"a"}]}`, string(got))
	_, err = h.HandleSubscribe(context.Background(), "u", json.RawMessage(`{"name":"a"}`))
	requireRPC(t, err, -32015, "CallbackEndpointError", `{"reason":"challenge_failed"}`)

	var paths []string
	for _, r := range s.requests {
		paths = append(paths, r.Method+" "+r.RawPath+"?"+r.Query)
	}
	assert.Equal(t, []string{
		"GET /api/v2/tenants/tenant_of_u/mcp/events?topics=a",
		"PUT /api/v2/tenants/tenant_of_u/mcp/subscriptions?",
	}, paths)
	assert.True(t, strings.Contains(s.requests[1].Body, `"allowed_topics":["a"]`))
}

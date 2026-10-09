package mcpevents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// The MCP Events request methods.
const (
	MethodList        = "events/list"
	MethodSubscribe   = "events/subscribe"
	MethodUnsubscribe = "events/unsubscribe"
)

// Options configures Handlers.
type Options struct {
	// ResolveTenant maps the authenticated principal to the Outpost tenant
	// whose events it may subscribe to. Required. Return "" when the
	// principal has no tenant; the request is then rejected with Forbidden.
	ResolveTenant func(ctx context.Context, principal string) (string, error)
	// AllowedTopics, when set, returns the topics the principal may discover
	// and subscribe to. events/list only returns these topics (an empty or
	// nil result answers with no events, without calling Outpost) and
	// events/subscribe answers NotFound for any other name, the same error as
	// an unknown topic. events/unsubscribe is never filtered, so cleanup
	// always works.
	AllowedTopics func(ctx context.Context, principal string) ([]string, error)
	// Codes are the JSON-RPC codes for the errors raised locally. Match
	// Outpost's MCP_ERROR_CODES. The zero value means CodesSketch.
	Codes ErrorCodes
	// OnError is called with every failure answered as a generic internal
	// error (Outpost unreachable, unexpected status, resolver errors), for
	// logging. MCP clients never see these details.
	OnError func(ctx context.Context, method string, err error)
}

// Handlers answer the MCP Events methods through the Outpost endpoints.
// Each handler takes the authenticated principal ("" when there is none,
// which is rejected with Forbidden) and the raw MCP params, and returns the
// MCP result as a raw JSON object or an *RPCError to send back as the
// JSON-RPC error. Every non-nil error they return is an *RPCError.
type Handlers struct {
	api   API
	opts  Options
	codes ErrorCodes
}

// NewHandlers returns the handlers for api, usually a *Client.
func NewHandlers(api API, opts Options) (*Handlers, error) {
	if api == nil {
		return nil, errors.New("mcpevents: an API client is required")
	}
	if opts.ResolveTenant == nil {
		return nil, errors.New("mcpevents: Options.ResolveTenant is required")
	}
	codes := opts.Codes
	if codes == (ErrorCodes{}) {
		codes = CodesSketch
	}
	return &Handlers{api: api, opts: opts, codes: codes}, nil
}

// Codes are the JSON-RPC codes in use for the errors raised locally.
func (h *Handlers) Codes() ErrorCodes { return h.codes }

// HandleList answers events/list.
func (h *Handlers) HandleList(ctx context.Context, principal string, params json.RawMessage) (json.RawMessage, error) {
	return h.run(ctx, MethodList, func() (json.RawMessage, error) {
		req, err := h.prepare(ctx, principal, params)
		if err != nil {
			return nil, err
		}
		var cursor string
		if raw, ok := req.fields["cursor"]; ok && !isNull(raw) {
			if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &cursor) != nil {
				return nil, h.invalidParams("cursor")
			}
		}
		topics, err := h.allowlist(ctx, principal)
		if err != nil {
			return nil, err
		}
		if topics != nil && len(topics) == 0 {
			return json.RawMessage(`{"events":[]}`), nil
		}
		return h.api.ListEvents(ctx, req.tenantID, ListEventsQuery{Cursor: cursor, Topics: topics})
	})
}

// HandleSubscribe answers events/subscribe.
func (h *Handlers) HandleSubscribe(ctx context.Context, principal string, params json.RawMessage) (json.RawMessage, error) {
	return h.run(ctx, MethodSubscribe, func() (json.RawMessage, error) {
		req, err := h.prepare(ctx, principal, params)
		if err != nil {
			return nil, err
		}
		topics, err := h.allowlist(ctx, principal)
		if err != nil {
			return nil, err
		}
		if topics != nil {
			// Every member a server could read as "name": Go's JSON decoding,
			// Outpost's included, matches member names case-insensitively,
			// so {"name":"allowed","Name":"hidden"} must not pass as "allowed".
			for key, raw := range req.fields {
				var name string
				if strings.EqualFold(key, "name") && json.Unmarshal(raw, &name) == nil && !slices.Contains(topics, name) {
					return nil, &RPCError{Code: h.codes.NotFound, Message: "NotFound", Data: json.RawMessage(`{"kind":"event"}`)}
				}
			}
		}
		// Outpost enforces the same list (AllowedTopics non-nil), covering
		// names that are not strings and any client that bypasses this check.
		return h.api.Subscribe(ctx, req.tenantID, SubscribeRequest{
			Principal:     principal,
			Params:        req.params,
			AllowedTopics: topics,
		})
	})
}

// HandleUnsubscribe answers events/unsubscribe. It is never filtered by
// AllowedTopics.
func (h *Handlers) HandleUnsubscribe(ctx context.Context, principal string, params json.RawMessage) (json.RawMessage, error) {
	return h.run(ctx, MethodUnsubscribe, func() (json.RawMessage, error) {
		req, err := h.prepare(ctx, principal, params)
		if err != nil {
			return nil, err
		}
		return h.api.Unsubscribe(ctx, req.tenantID, UnsubscribeRequest{Principal: principal, Params: req.params})
	})
}

// Handle dispatches one of the MCP Events methods by name.
func (h *Handlers) Handle(ctx context.Context, method, principal string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case MethodList:
		return h.HandleList(ctx, principal, params)
	case MethodSubscribe:
		return h.HandleSubscribe(ctx, principal, params)
	case MethodUnsubscribe:
		return h.HandleUnsubscribe(ctx, principal, params)
	}
	return nil, &RPCError{Code: -32601, Message: "Method not found"}
}

type prepared struct {
	tenantID string
	params   json.RawMessage
	fields   map[string]json.RawMessage
}

func (h *Handlers) prepare(ctx context.Context, principal string, params json.RawMessage) (prepared, error) {
	if principal == "" {
		return prepared{}, h.forbidden()
	}
	params = bytes.TrimSpace(params)
	if len(params) == 0 || isNull(params) {
		params = json.RawMessage("{}")
	}
	var fields map[string]json.RawMessage
	if params[0] != '{' || json.Unmarshal(params, &fields) != nil {
		return prepared{}, h.invalidParams("params")
	}
	tenantID, err := h.opts.ResolveTenant(ctx, principal)
	if err != nil {
		return prepared{}, err
	}
	if tenantID == "" {
		return prepared{}, h.forbidden()
	}
	return prepared{tenantID: tenantID, params: params, fields: fields}, nil
}

// allowlist returns nil when AllowedTopics is unset, else a non-nil slice.
func (h *Handlers) allowlist(ctx context.Context, principal string) ([]string, error) {
	if h.opts.AllowedTopics == nil {
		return nil, nil
	}
	topics, err := h.opts.AllowedTopics(ctx, principal)
	if err != nil {
		return nil, err
	}
	// Topic names are never empty and never contain commas (the list is sent
	// comma-separated), so such entries can only fail to match; dropping them
	// keeps the allowlist from widening.
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		if t != "" && !strings.Contains(t, ",") {
			out = append(out, t)
		}
	}
	return out, nil
}

func (h *Handlers) run(ctx context.Context, method string, fn func() (json.RawMessage, error)) (json.RawMessage, error) {
	result, err := fn()
	if err == nil {
		if isJSONObject(result) {
			return result, nil
		}
		err = errors.New("mcpevents: " + method + " result is not a JSON object")
	}
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && rpcErr != nil {
		return nil, rpcErr
	}
	if m, ok := MCPErrorFrom(err); ok {
		return nil, &RPCError{Code: m.Code, Message: m.Message, Data: m.Data}
	}
	h.Report(ctx, method, err)
	return nil, h.InternalError()
}

// Report passes err to Options.OnError, if set. A panicking hook is
// recovered: logging must not change the answer.
func (h *Handlers) Report(ctx context.Context, method string, err error) {
	if h.opts.OnError == nil {
		return
	}
	defer func() { _ = recover() }()
	h.opts.OnError(ctx, method, err)
}

// InternalError is the generic error answered for unexpected failures.
func (h *Handlers) InternalError() *RPCError {
	return &RPCError{Code: h.codes.InternalError, Message: "Internal error"}
}

// Forbidden is the error answered when there is no principal or tenant.
func (h *Handlers) Forbidden() *RPCError { return h.forbidden() }

func (h *Handlers) forbidden() *RPCError {
	return &RPCError{Code: h.codes.Forbidden, Message: "Forbidden"}
}

func (h *Handlers) invalidParams(field string) *RPCError {
	data, _ := json.Marshal(map[string]string{"field": field, "reason": "invalid"})
	return &RPCError{Code: h.codes.InvalidParams, Message: "InvalidParams", Data: data}
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

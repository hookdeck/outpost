package mcpevents

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Kind is the stable name of an MCP error. The JSON-RPC code it maps to
// depends on the CodeProfile.
type Kind string

const (
	KindInvalidParams         Kind = "invalid_params"
	KindNotFound              Kind = "not_found"
	KindResourceExhausted     Kind = "resource_exhausted"
	KindUnsupported           Kind = "unsupported"
	KindCallbackEndpointError Kind = "callback_endpoint_error"
	// KindForbidden is never returned by the endpoints (the operator
	// authorizes before calling Outpost); it only appears in terminated
	// envelopes for revoked subscriptions.
	KindForbidden Kind = "forbidden"
)

// CodeProfile selects the JSON-RPC code numbers (MCP_ERROR_CODES).
type CodeProfile string

const (
	// CodeProfileSketch is the design sketch numbering ChatGPT follows.
	CodeProfileSketch CodeProfile = "sketch"
	// CodeProfileSEP3415 is SEP-3415's provisional numbering.
	CodeProfileSEP3415 CodeProfile = "sep-3415"
)

// ParseCodeProfile parses an MCP_ERROR_CODES value; "" means sketch.
func ParseCodeProfile(s string) (CodeProfile, error) {
	switch CodeProfile(s) {
	case "", CodeProfileSketch:
		return CodeProfileSketch, nil
	case CodeProfileSEP3415:
		return CodeProfileSEP3415, nil
	}
	return "", fmt.Errorf("mcpevents: unknown error code profile %q (want %q or %q)", s, CodeProfileSketch, CodeProfileSEP3415)
}

type kindInfo struct {
	message string
	sketch  int
	sep     int
}

var kinds = map[Kind]kindInfo{
	KindInvalidParams:         {"InvalidParams", -32602, -32602},
	KindNotFound:              {"NotFound", -32011, -32023},
	KindForbidden:             {"Forbidden", -32012, -32024},
	KindResourceExhausted:     {"ResourceExhausted", -32013, -32025},
	KindUnsupported:           {"Unsupported", -32014, -32026},
	KindCallbackEndpointError: {"CallbackEndpointError", -32015, -32027},
}

// ForbiddenCode returns the Forbidden code of a profile, for terminated
// envelopes.
func ForbiddenCode(profile CodeProfile) int {
	return kindCode(KindForbidden, profile)
}

func kindCode(k Kind, profile CodeProfile) int {
	info, ok := kinds[k]
	if !ok {
		return -32603 // InternalError; unreachable with the constructors below
	}
	if profile == CodeProfileSEP3415 {
		return info.sep
	}
	return info.sketch
}

// Reasons and other fixed data values. Data never carries client payload
// values, resolved addresses or raw endpoint responses.
const (
	// invalid_params reasons.
	ReasonRequired          = "required"
	ReasonInvalidType       = "invalid_type"
	ReasonInvalid           = "invalid" // malformed: duplicate keys, number out of range
	ReasonTooLarge          = "too_large"
	ReasonInvalidSecret     = "invalid_secret"
	ReasonSchemaMismatch    = "schema_mismatch"
	ReasonConflict          = "conflict"
	ReasonInvalidURL        = "invalid_url"
	ReasonHTTPSRequired     = "https_required"
	ReasonAddressNotAllowed = "address_not_allowed"

	// callback_endpoint_error reasons, also deliveryStatus.lastError.
	ReasonConnectionRefused = "connection_refused"
	ReasonTimeout           = "timeout"
	ReasonTLSError          = "tls_error"
	ReasonHTTP4xx           = "http_4xx"
	ReasonHTTP5xx           = "http_5xx"
	ReasonChallengeFailed   = "challenge_failed"

	// not_found kinds.
	NotFoundEvent        = "event"
	NotFoundTenant       = "tenant"
	NotFoundSubscription = "subscription"

	// resource_exhausted limits.
	LimitSubscriptions          = "subscriptions"
	LimitPrincipalSubscriptions = "principal_subscriptions"
	LimitVerificationRate       = "verification_rate"
)

// Error is an MCP-level failure, rendered as {"mcp_error":{...}} (HTTP 422)
// for the operator to rethrow as a JSON-RPC error. Build it with the
// constructors below so Data keeps its fixed shape.
type Error struct {
	Kind    Kind
	Message string
	Data    map[string]any
	// Profile selects the code numbers; "" means sketch.
	Profile CodeProfile
}

func newError(k Kind, data map[string]any) *Error {
	return &Error{Kind: k, Message: kinds[k].message, Data: data}
}

// InvalidParams reports a statically invalid request: data {field, reason}.
func InvalidParams(field, reason string) *Error {
	return newError(KindInvalidParams, map[string]any{"field": field, "reason": reason})
}

// InvalidParamsWithErrors is InvalidParams with validation messages (which
// never contain instance values): data {field, reason, errors}.
func InvalidParamsWithErrors(field, reason string, errs []string) *Error {
	e := InvalidParams(field, reason)
	if len(errs) > 0 {
		e.Data["errors"] = append([]string(nil), errs...)
	}
	return e
}

// NotFound reports a missing entity: data {kind: "event"|"tenant"|"subscription"}.
func NotFound(kind string) *Error {
	return newError(KindNotFound, map[string]any{"kind": kind})
}

// ResourceExhausted reports a limit: data {limit, max?, retryAfterMs?}; max and
// retryAfter are omitted when not positive.
func ResourceExhausted(limit string, max int, retryAfter time.Duration) *Error {
	data := map[string]any{"limit": limit}
	if max > 0 {
		data["max"] = max
	}
	if ms := retryAfter.Milliseconds(); ms > 0 {
		data["retryAfterMs"] = ms
	}
	return newError(KindResourceExhausted, data)
}

// Unsupported reports an unsupported option: data {feature, value}. value
// echoes the client's own input, so it is limited to scalars (long strings
// are truncated); objects and arrays become null.
func Unsupported(feature string, value any) *Error {
	switch v := value.(type) {
	case nil, bool, float64, json.Number:
	case string:
		value = truncate(v, 64)
	default:
		value = nil
	}
	return newError(KindUnsupported, map[string]any{"feature": feature, "value": value})
}

// CallbackEndpointError reports a failed verification: data {reason}, one of
// the fixed lastError categories.
func CallbackEndpointError(reason string) *Error {
	return newError(KindCallbackEndpointError, map[string]any{"reason": reason})
}

// Termination errors carried by terminated envelopes.

// AccessRevoked is the Forbidden error for a revoked subscription.
func AccessRevoked() *Error {
	return newError(KindForbidden, map[string]any{"reason": "access_revoked"})
}

// EventEnded is the NotFound error for a subscription whose event type is no
// longer offered.
func EventEnded() *Error {
	return NotFound(NotFoundEvent)
}

// SchemaChanged is the Unsupported error for a subscription ended by a
// breaking payload schema change.
func SchemaChanged() *Error {
	return newError(KindUnsupported, map[string]any{"feature": "payloadSchema", "reason": "schema_changed"})
}

// Code returns the JSON-RPC code for the error's profile.
func (e *Error) Code() int {
	return kindCode(e.Kind, e.Profile)
}

// WithProfile returns a copy of e that renders codes from profile.
func (e *Error) WithProfile(profile CodeProfile) *Error {
	c := *e
	c.Profile = profile
	return &c
}

func (e *Error) Error() string {
	return fmt.Sprintf("mcpevents: %s %v", e.Kind, e.Data)
}

// RPCError is the JSON-RPC error object: the body of mcp_error minus kind, and
// the error member of a terminated envelope.
type RPCError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

// RPCError returns the JSON-RPC error object for e.
func (e *Error) RPCError() RPCError {
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	return RPCError{Code: e.Code(), Message: e.Message, Data: data}
}

// errorBody is the HTTP body of an MCP-level failure.
type errorBody struct {
	MCPError errorObject `json:"mcp_error"`
}

type errorObject struct {
	Kind    Kind           `json:"kind"`
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

// MarshalJSON renders the HTTP body {"mcp_error":{"kind","code","message","data"}}.
func (e Error) MarshalJSON() ([]byte, error) {
	rpc := e.RPCError()
	return marshalNoEscape(errorBody{MCPError: errorObject{Kind: e.Kind, Code: rpc.Code, Message: rpc.Message, Data: rpc.Data}})
}

// LastErrorCategory maps an attempt code to the fixed lastError categories
// (deliveryStatus.lastError): 5xx → http_5xx; 3xx, 4xx and payload_too_large →
// http_4xx; timeout → timeout; tls_error → tls_error; anything else
// (connection errors, address_not_allowed, throttled, ...) → connection_refused.
func LastErrorCategory(code string) string {
	switch code {
	case "payload_too_large":
		return ReasonHTTP4xx
	case "timeout":
		return ReasonTimeout
	case "tls_error":
		return ReasonTLSError
	}
	if len(code) == 3 {
		if status, err := strconv.Atoi(code); err == nil {
			switch {
			case status >= 500 && status <= 599:
				return ReasonHTTP5xx
			case status >= 300 && status <= 499:
				return ReasonHTTP4xx
			}
		}
	}
	return ReasonConnectionRefused
}

// AttemptLastError returns the lastError category of an attempt, or nil when
// the attempt succeeded (status "success").
func AttemptLastError(status, code string) *string {
	if status == "success" {
		return nil
	}
	category := LastErrorCategory(code)
	return &category
}

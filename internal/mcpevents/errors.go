package mcpevents

import (
	"fmt"
	"time"
)

// Kind is the stable name of an MCP error (SEP-3415).
type Kind string

const (
	KindInvalidParams         Kind = "invalid_params"
	KindNotFound              Kind = "not_found"
	KindForbidden             Kind = "forbidden"
	KindResourceExhausted     Kind = "resource_exhausted"
	KindUnsupported           Kind = "unsupported"
	KindCallbackEndpointError Kind = "callback_endpoint_error"
)

type kindInfo struct {
	message string
	code    int
}

var kinds = map[Kind]kindInfo{
	KindInvalidParams:         {"InvalidParams", -32602},
	KindNotFound:              {"NotFound", -32023},
	KindForbidden:             {"Forbidden", -32024},
	KindResourceExhausted:     {"ResourceExhausted", -32025},
	KindUnsupported:           {"Unsupported", -32026},
	KindCallbackEndpointError: {"CallbackEndpointError", -32027},
}

const codeInternalError = -32603

// Reasons and other fixed data values. Data never carries resolved addresses
// or raw endpoint responses, only fixed values and names from the client's own
// request (a field path, the unsupported delivery mode).
const (
	// Fields named by invalid_params.
	FieldURL    = "config.url"
	FieldSecret = "credentials.secret"

	// invalid_params reasons.
	ReasonInvalidSecret     = "invalid_secret"
	ReasonInvalidURL        = "invalid_url"
	ReasonHTTPSRequired     = "https_required"
	ReasonAddressNotAllowed = "address_not_allowed"
	ReasonRequired          = "required"
	ReasonInvalidType       = "invalid_type"
	ReasonInvalid           = "invalid"
	ReasonTooLarge          = "too_large"

	// forbidden reasons.
	ReasonUnauthenticated = "unauthenticated"
	ReasonTopicNotAllowed = "topic_not_allowed"

	// callback_endpoint_error reasons: the fixed lastError categories.
	ReasonConnectionRefused = "connection_refused"
	ReasonTimeout           = "timeout"
	ReasonTLSError          = "tls_error"
	ReasonHTTP4xx           = "http_4xx"
	ReasonHTTP5xx           = "http_5xx"
	ReasonChallengeFailed   = "challenge_failed"

	// not_found kinds.
	NotFoundEvent        = "event"
	NotFoundSubscription = "subscription"

	// resource_exhausted limits.
	LimitVerificationRate = "verification_rate"
	LimitSubscriptions    = "subscriptions"
)

// wireData lists the Data keys each kind sends on the wire: the fields the MCP
// Events spec defines for it. Other kinds send no data.
var wireData = map[Kind][]string{
	KindNotFound:              {"kind"},
	KindUnsupported:           {"feature", "value"},
	KindCallbackEndpointError: {"reason"},
	KindResourceExhausted:     {"limit", "max"},
}

// Error is an MCP-level failure. Build it with the constructors below so
// Data keeps its fixed shape. Data also serves the REST mapping and logs; only
// the keys in wireData reach MCP clients.
type Error struct {
	Kind    Kind
	Message string
	Data    map[string]any
}

func newError(k Kind, data map[string]any) *Error {
	return &Error{Kind: k, Message: kinds[k].message, Data: data}
}

// InvalidParams reports a statically invalid request: data {field, reason}.
func InvalidParams(field, reason string) *Error {
	return newError(KindInvalidParams, map[string]any{"field": field, "reason": reason})
}

// NotFound reports a missing entity: data {kind}.
func NotFound(kind string) *Error {
	return newError(KindNotFound, map[string]any{"kind": kind})
}

// Unsupported reports a feature the server doesn't offer: data {feature, value}.
func Unsupported(feature, value string) *Error {
	return newError(KindUnsupported, map[string]any{"feature": feature, "value": value})
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

// CallbackEndpointError reports a failed verification: data {reason}, one of
// the fixed lastError categories.
func CallbackEndpointError(reason string) *Error {
	return newError(KindCallbackEndpointError, map[string]any{"reason": reason})
}

// Forbidden reports a request without a usable principal: data {reason}.
func Forbidden(reason string) *Error {
	return newError(KindForbidden, map[string]any{"reason": reason})
}

// RPCError returns the JSON-RPC error object: code, message and, when the kind
// defines any, data.
func (e *Error) RPCError() map[string]any {
	out := map[string]any{"code": e.Code(), "message": e.Message}
	data := map[string]any{}
	for _, key := range wireData[e.Kind] {
		if v, ok := e.Data[key]; ok {
			data[key] = v
		}
	}
	if len(data) > 0 {
		out["data"] = data
	}
	return out
}

// Code returns the JSON-RPC error code of the error's kind.
func (e *Error) Code() int {
	if info, ok := kinds[e.Kind]; ok {
		return info.code
	}
	return codeInternalError
}

// Reason returns the data's reason, or its limit for resource_exhausted.
func (e *Error) Reason() string {
	if r, ok := e.Data["reason"].(string); ok {
		return r
	}
	l, _ := e.Data["limit"].(string)
	return l
}

func (e *Error) Error() string {
	return fmt.Sprintf("mcpevents: %s %v", e.Kind, e.Data)
}

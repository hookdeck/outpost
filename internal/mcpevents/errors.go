package mcpevents

import (
	"fmt"
	"time"
)

// Kind is the stable name of an MCP error (SEP-3415).
type Kind string

const (
	KindInvalidParams         Kind = "invalid_params"
	KindResourceExhausted     Kind = "resource_exhausted"
	KindCallbackEndpointError Kind = "callback_endpoint_error"
)

var messages = map[Kind]string{
	KindInvalidParams:         "InvalidParams",
	KindResourceExhausted:     "ResourceExhausted",
	KindCallbackEndpointError: "CallbackEndpointError",
}

// Reasons and other fixed data values. Data never carries client payload
// values, resolved addresses or raw endpoint responses.
const (
	// Fields named by invalid_params.
	FieldURL    = "config.url"
	FieldSecret = "credentials.secret"

	// invalid_params reasons.
	ReasonInvalidSecret     = "invalid_secret"
	ReasonInvalidURL        = "invalid_url"
	ReasonHTTPSRequired     = "https_required"
	ReasonAddressNotAllowed = "address_not_allowed"

	// callback_endpoint_error reasons: the fixed lastError categories.
	ReasonConnectionRefused = "connection_refused"
	ReasonTimeout           = "timeout"
	ReasonTLSError          = "tls_error"
	ReasonHTTP4xx           = "http_4xx"
	ReasonHTTP5xx           = "http_5xx"
	ReasonChallengeFailed   = "challenge_failed"

	// resource_exhausted limits.
	LimitVerificationRate = "verification_rate"
)

// Error is an MCP-level failure. Build it with the constructors below so
// Data keeps its fixed shape.
type Error struct {
	Kind    Kind
	Message string
	Data    map[string]any
}

func newError(k Kind, data map[string]any) *Error {
	return &Error{Kind: k, Message: messages[k], Data: data}
}

// InvalidParams reports a statically invalid request: data {field, reason}.
func InvalidParams(field, reason string) *Error {
	return newError(KindInvalidParams, map[string]any{"field": field, "reason": reason})
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

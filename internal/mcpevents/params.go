package mcpevents

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const (
	// MaxParamsBytes caps the raw params object. A valid one is a few KiB:
	// arguments (≤ MaxArgumentsBytes canonical), a URL, a secret and a name.
	MaxParamsBytes = 64 << 10
	// MaxArgumentsBytes caps the canonical arguments.
	MaxArgumentsBytes = 4 << 10
	// maxParamsDepth bounds nesting in params; arguments need three levels
	// (an operator object inside the arguments object inside params).
	maxParamsDepth = 16

	// SecretPrefix starts a Standard Webhooks symmetric secret.
	SecretPrefix = "whsec_"
	// Decoded secret length bounds, from the MCP Events spec.
	MinSecretBytes = 24
	MaxSecretBytes = 64

	// DeliveryModeWebhook is the only delivery mode subscribe supports.
	DeliveryModeWebhook = "webhook"
)

// invalid_params fields.
const (
	FieldParams         = "params"
	FieldName           = "name"
	FieldArguments      = "arguments"
	FieldDelivery       = "delivery"
	FieldDeliverySecret = "delivery.secret"
	FieldTTLMs          = "ttlMs"
)

// TTLKind says how a subscribe request suggested a lifetime.
type TTLKind int

const (
	// TTLAbsent: ttlMs omitted, the server default applies.
	TTLAbsent TTLKind = iota
	// TTLNull: ttlMs null, a request for no expiry.
	TTLNull
	// TTLFinite: ttlMs is a number (any finite value, clamped by GrantTTL).
	TTLFinite
)

// TTLRequest is the parsed ttlMs.
type TTLRequest struct {
	Kind TTLKind
	// Ms is the suggested lifetime when Kind is TTLFinite. Values beyond
	// float64 range arrive as ±math.MaxFloat64.
	Ms float64
}

// Delivery is the parsed params.delivery of a subscribe request.
type Delivery struct {
	// Mode is always DeliveryModeWebhook.
	Mode string
	// URL and URLString are the normalized callback URL.
	URL       *url.URL
	URLString string
	// Secret is the whsec_ value as sent; SecretKey its decoded bytes.
	Secret    string
	SecretKey []byte
}

// SubscribeParams is a parsed events/subscribe params object.
type SubscribeParams struct {
	Name string
	// Arguments is the canonical JSON of the arguments object ({} when
	// omitted), the form the subscription ID and storage use.
	Arguments json.RawMessage
	// ArgumentsMap is the decoded arguments (numbers as float64), for
	// inputSchema validation and ArgumentsToFilter.
	ArgumentsMap map[string]any
	Delivery     Delivery
	TTL          TTLRequest
}

// UnsubscribeParams is a parsed events/unsubscribe params object.
type UnsubscribeParams struct {
	Name         string
	Arguments    json.RawMessage
	ArgumentsMap map[string]any
	URL          *url.URL
	URLString    string
}

// ParseSubscribeParams parses events/subscribe params. Unknown fields (cursor,
// maxAgeMs, _meta, ...) are ignored; keys match exactly (encoding/json's
// case-insensitive matching would let {"name":"a","NAME":"b"} read differently
// here than in the operator's JavaScript server) and duplicate keys are
// rejected. Failures are *Error: invalid_params, or unsupported for a delivery
// mode other than webhook.
func ParseSubscribeParams(raw json.RawMessage) (*SubscribeParams, error) {
	obj, err := parseParamsObject(raw)
	if err != nil {
		return nil, err
	}
	name, err := parseName(obj)
	if err != nil {
		return nil, err
	}
	args, argsMap, err := parseArguments(obj)
	if err != nil {
		return nil, err
	}
	delivery, err := deliveryObject(obj)
	if err != nil {
		return nil, err
	}
	if mode, ok := delivery["mode"]; ok && mode != DeliveryModeWebhook {
		return nil, Unsupported("deliveryMode", mode)
	}
	u, us, err := parseDeliveryURL(delivery)
	if err != nil {
		return nil, err
	}
	secret, ok := delivery["secret"].(string)
	if !ok {
		return nil, InvalidParams(FieldDeliverySecret, ReasonInvalidSecret)
	}
	key, err := DecodeSecret(secret)
	if err != nil {
		return nil, InvalidParams(FieldDeliverySecret, ReasonInvalidSecret)
	}
	ttl, err := parseTTL(obj)
	if err != nil {
		return nil, err
	}
	return &SubscribeParams{
		Name:         name,
		Arguments:    args,
		ArgumentsMap: argsMap,
		Delivery: Delivery{
			Mode:      DeliveryModeWebhook,
			URL:       u,
			URLString: us,
			Secret:    secret,
			SecretKey: key,
		},
		TTL: ttl,
	}, nil
}

// ParseUnsubscribeParams parses events/unsubscribe params: name, arguments
// and delivery.url, with the same rules as subscribe. delivery.mode and
// delivery.secret are ignored.
func ParseUnsubscribeParams(raw json.RawMessage) (*UnsubscribeParams, error) {
	obj, err := parseParamsObject(raw)
	if err != nil {
		return nil, err
	}
	name, err := parseName(obj)
	if err != nil {
		return nil, err
	}
	args, argsMap, err := parseArguments(obj)
	if err != nil {
		return nil, err
	}
	delivery, err := deliveryObject(obj)
	if err != nil {
		return nil, err
	}
	u, us, err := parseDeliveryURL(delivery)
	if err != nil {
		return nil, err
	}
	return &UnsubscribeParams{Name: name, Arguments: args, ArgumentsMap: argsMap, URL: u, URLString: us}, nil
}

func parseParamsObject(raw json.RawMessage) (map[string]any, error) {
	if len(raw) > MaxParamsBytes {
		return nil, InvalidParams(FieldParams, ReasonTooLarge)
	}
	v, err := decodeStrict(raw, maxParamsDepth)
	if err != nil {
		if len(raw) == 0 {
			return nil, InvalidParams(FieldParams, ReasonRequired)
		}
		return nil, InvalidParams(FieldParams, ReasonInvalid)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, InvalidParams(FieldParams, ReasonInvalidType)
	}
	return obj, nil
}

func parseName(obj map[string]any) (string, error) {
	v, ok := obj["name"]
	if !ok || v == nil {
		return "", InvalidParams(FieldName, ReasonRequired)
	}
	name, ok := v.(string)
	if !ok {
		return "", InvalidParams(FieldName, ReasonInvalidType)
	}
	if name == "" {
		return "", InvalidParams(FieldName, ReasonRequired)
	}
	return name, nil
}

func parseArguments(obj map[string]any) (json.RawMessage, map[string]any, error) {
	v := obj["arguments"]
	if v == nil { // absent or null
		return json.RawMessage("{}"), map[string]any{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, InvalidParams(FieldArguments, ReasonInvalidType)
	}
	if _, err := numbersToFloat(m); err != nil {
		return nil, nil, InvalidParams(FieldArguments, ReasonInvalid)
	}
	canonical, err := CanonicalJSON(m)
	if err != nil {
		return nil, nil, InvalidParams(FieldArguments, ReasonInvalid)
	}
	if len(canonical) > MaxArgumentsBytes {
		return nil, nil, InvalidParams(FieldArguments, ReasonTooLarge)
	}
	return canonical, m, nil
}

func deliveryObject(obj map[string]any) (map[string]any, error) {
	v, ok := obj["delivery"]
	if !ok || v == nil {
		return nil, InvalidParams(FieldDelivery, ReasonRequired)
	}
	d, ok := v.(map[string]any)
	if !ok {
		return nil, InvalidParams(FieldDelivery, ReasonInvalidType)
	}
	return d, nil
}

func parseDeliveryURL(delivery map[string]any) (*url.URL, string, error) {
	raw, ok := delivery["url"].(string)
	if !ok {
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	}
	return NormalizeCallbackURL(raw)
}

func parseTTL(obj map[string]any) (TTLRequest, error) {
	v, ok := obj["ttlMs"]
	if !ok {
		return TTLRequest{Kind: TTLAbsent}, nil
	}
	switch v := v.(type) {
	case nil:
		return TTLRequest{Kind: TTLNull}, nil
	case json.Number:
		// No rejection path for TTL values: any number is clamped later,
		// including ones beyond float64 range.
		f, err := strconv.ParseFloat(string(v), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return TTLRequest{}, InvalidParams(FieldTTLMs, ReasonInvalidType)
		}
		if math.IsInf(f, 0) {
			f = math.Copysign(math.MaxFloat64, f)
		}
		return TTLRequest{Kind: TTLFinite, Ms: f}, nil
	default:
		return TTLRequest{}, InvalidParams(FieldTTLMs, ReasonInvalidType)
	}
}

// DecodeSecret decodes a Standard Webhooks symmetric secret: "whsec_" followed
// by standard-alphabet base64 (padded or unpadded) of MinSecretBytes to
// MaxSecretBytes bytes. The URL-safe alphabet, whitespace and line breaks are
// rejected (encoding/base64 would silently skip CR and LF).
func DecodeSecret(secret string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(secret, SecretPrefix)
	if !ok {
		return nil, errInvalidSecret
	}
	data := strings.TrimRight(encoded, "=")
	if padding := len(encoded) - len(data); padding > 2 || (padding > 0 && len(encoded)%4 != 0) {
		return nil, errInvalidSecret
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !('A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '+' || c == '/') {
			return nil, errInvalidSecret
		}
	}
	key, err := base64.RawStdEncoding.DecodeString(data)
	if err != nil || len(key) < MinSecretBytes || len(key) > MaxSecretBytes {
		return nil, errInvalidSecret
	}
	return key, nil
}

var errInvalidSecret = errors.New("mcpevents: secret must be whsec_ followed by base64 of 24 to 64 bytes")

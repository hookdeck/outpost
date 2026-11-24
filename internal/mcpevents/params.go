package mcpevents

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

const (
	// maxParamsBytes is checked before the params are decoded.
	maxParamsBytes = 64 << 10
	// maxParamsDepth caps nesting in the params, including members that are
	// ignored (_meta, ...).
	maxParamsDepth = 16

	deliveryModeWebhook = "webhook"
)

// invalid_params fields.
const (
	FieldParams       = "params"
	FieldName         = "name"
	FieldArguments    = "arguments"
	FieldDelivery     = "delivery"
	FieldDeliveryMode = "delivery.mode"
	FieldTTLMs        = "ttlMs"
)

// TTLKind says how a subscribe request suggested a lifetime.
type TTLKind int

const (
	TTLAbsent TTLKind = iota
	TTLNull
	TTLFinite
)

// TTLRequest is the parsed ttlMs.
type TTLRequest struct {
	Kind TTLKind
	// Ms is the suggested lifetime when Kind is TTLFinite; a value beyond
	// float64 range arrives as ±math.MaxFloat64.
	Ms float64
}

// SubscribeParams is a parsed events/subscribe params object.
type SubscribeParams struct {
	Name string
	// Arguments is the canonical JSON of the arguments object ({} when omitted).
	Arguments json.RawMessage
	// URL is the normalized callback URL.
	URL    string
	Secret string
	TTL    TTLRequest
}

// UnsubscribeParams is a parsed events/unsubscribe params object.
type UnsubscribeParams struct {
	Name      string
	Arguments json.RawMessage
	URL       string
}

// ParseSubscribeParams parses events/subscribe params. Unknown fields (cursor,
// maxAgeMs, _meta, ...) are ignored. Keys match exactly and duplicate keys are
// rejected, so the params read the same here as in the MCP server that
// forwards them. Failures are *Error invalid_params.
func ParseSubscribeParams(raw json.RawMessage) (*SubscribeParams, error) {
	obj, err := parseParamsObject(raw)
	if err != nil {
		return nil, err
	}
	name, args, err := parseNameAndArguments(obj)
	if err != nil {
		return nil, err
	}
	delivery, err := deliveryObject(obj)
	if err != nil {
		return nil, err
	}
	if mode, ok := delivery["mode"]; ok && mode != deliveryModeWebhook {
		if modeName, isString := mode.(string); isString {
			return nil, Unsupported("deliveryMode", modeName)
		}
		return nil, InvalidParams(FieldDeliveryMode, ReasonInvalid)
	}
	normalized, err := parseDeliveryURL(delivery)
	if err != nil {
		return nil, err
	}
	secret, ok := delivery["secret"].(string)
	if !ok {
		return nil, InvalidParams(FieldSecret, ReasonInvalidSecret)
	}
	if _, err := DecodeSecret(secret); err != nil {
		return nil, InvalidParams(FieldSecret, ReasonInvalidSecret)
	}
	ttl, err := parseTTL(obj)
	if err != nil {
		return nil, err
	}
	return &SubscribeParams{Name: name, Arguments: args, URL: normalized, Secret: secret, TTL: ttl}, nil
}

// ParseUnsubscribeParams parses events/unsubscribe params: name, arguments and
// delivery.url, with the same rules as subscribe.
func ParseUnsubscribeParams(raw json.RawMessage) (*UnsubscribeParams, error) {
	obj, err := parseParamsObject(raw)
	if err != nil {
		return nil, err
	}
	name, args, err := parseNameAndArguments(obj)
	if err != nil {
		return nil, err
	}
	delivery, err := deliveryObject(obj)
	if err != nil {
		return nil, err
	}
	normalized, err := parseDeliveryURL(delivery)
	if err != nil {
		return nil, err
	}
	return &UnsubscribeParams{Name: name, Arguments: args, URL: normalized}, nil
}

func parseParamsObject(raw json.RawMessage) (map[string]any, error) {
	if len(raw) > maxParamsBytes {
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

func parseNameAndArguments(obj map[string]any) (string, json.RawMessage, error) {
	v, ok := obj["name"]
	if !ok || v == nil {
		return "", nil, InvalidParams(FieldName, ReasonRequired)
	}
	name, ok := v.(string)
	if !ok {
		return "", nil, InvalidParams(FieldName, ReasonInvalidType)
	}
	if name == "" {
		return "", nil, InvalidParams(FieldName, ReasonRequired)
	}
	args, err := parseArguments(obj)
	return name, args, err
}

func parseArguments(obj map[string]any) (json.RawMessage, error) {
	v := obj["arguments"]
	if v == nil {
		return json.RawMessage("{}"), nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, InvalidParams(FieldArguments, ReasonInvalidType)
	}
	if _, err := numbersToFloat(m); err != nil {
		return nil, InvalidParams(FieldArguments, ReasonInvalid)
	}
	canonical, err := CanonicalJSON(m)
	if err != nil {
		return nil, InvalidParams(FieldArguments, ReasonInvalid)
	}
	return canonical, nil
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

func parseDeliveryURL(delivery map[string]any) (string, error) {
	raw, ok := delivery["url"].(string)
	if !ok {
		return "", InvalidParams(FieldURL, ReasonInvalidURL)
	}
	_, normalized, err := NormalizeCallbackURL(raw)
	return normalized, err
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
		// Any number is accepted and clamped later, including one beyond float64 range.
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

// numbersToFloat replaces every json.Number in v (as produced by decodeStrict)
// with its float64 value, in place.
func numbersToFloat(v any) (any, error) {
	switch v := v.(type) {
	case json.Number:
		return parseJSONNumber(string(v))
	case []any:
		for i, e := range v {
			f, err := numbersToFloat(e)
			if err != nil {
				return nil, err
			}
			v[i] = f
		}
		return v, nil
	case map[string]any:
		for k, e := range v {
			f, err := numbersToFloat(e)
			if err != nil {
				return nil, err
			}
			v[k] = f
		}
		return v, nil
	default:
		return v, nil
	}
}

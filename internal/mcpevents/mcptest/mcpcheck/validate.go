package mcpcheck

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
)

type object map[string]json.RawMessage

func parseObject(raw []byte) (object, bool) {
	var o object
	if json.Unmarshal(raw, &o) != nil || o == nil {
		return nil, false
	}
	return o, true
}

func (o object) is(key, kind string) (present, ok bool) {
	raw, present := o[key]
	if !present {
		return false, false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return true, false
	}
	switch kind {
	case "string":
		_, ok = v.(string)
	case "bool":
		_, ok = v.(bool)
	case "object":
		_, ok = v.(map[string]any)
	case "array":
		_, ok = v.([]any)
	case "null":
		ok = v == nil
	case "integer":
		f, isNum := v.(float64)
		ok = isNum && f == float64(int64(f))
	}
	return true, ok
}

func (o object) str(key string) string {
	var s string
	_ = json.Unmarshal(o[key], &s)
	return s
}

func isTimestamp(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

// validateExtension checks the server/discover capabilities.
func validateExtension(res mcpclient.DiscoverResult) []string {
	raw, ok := res.Capabilities.Extensions[mcpclient.ExtensionID]
	if !ok {
		return []string{fmt.Sprintf("capabilities.extensions has no %q", mcpclient.ExtensionID)}
	}
	ext, isObj := parseObject(raw)
	if !isObj {
		return []string{"the events extension is not an object"}
	}
	if present, ok := ext.is("listChanged", "bool"); present && !ok {
		return []string{"listChanged is not a boolean"}
	}
	return nil
}

var deliveryModes = []string{"poll", "push", "webhook"}

// validateDescriptor checks one event of events/list.
func validateDescriptor(raw json.RawMessage) []string {
	d, ok := parseObject(raw)
	if !ok {
		return []string{"event is not an object"}
	}
	var out []string
	name := d.str("name")
	if name == "" {
		out = append(out, "event without name")
	}
	var modes []string
	if json.Unmarshal(d["delivery"], &modes) != nil || len(modes) == 0 {
		out = append(out, fmt.Sprintf("%s: delivery must be a non-empty array", name))
	}
	for _, m := range modes {
		if !slices.Contains(deliveryModes, m) {
			out = append(out, fmt.Sprintf("%s: unknown delivery mode %q", name, m))
		}
	}
	for _, k := range []string{"inputSchema", "payloadSchema"} {
		if present, ok := d.is(k, "object"); present && !ok {
			out = append(out, fmt.Sprintf("%s: %s is not an object", name, k))
		}
	}
	return out
}

// validateSubscribeResult checks the result of events/subscribe.
func validateSubscribeResult(raw json.RawMessage) []string {
	res, ok := parseObject(raw)
	if !ok {
		return []string{"result is not an object"}
	}
	var out []string
	if res.str("resultType") != "complete" {
		out = append(out, fmt.Sprintf("resultType %q, want complete", res.str("resultType")))
	}
	if res.str("id") == "" {
		out = append(out, "id is empty")
	}
	if !isTimestamp(res["refreshBefore"]) {
		out = append(out, fmt.Sprintf("refreshBefore %s is not an ISO 8601 timestamp", res["refreshBefore"]))
	}
	if present, ok := res.is("truncated", "bool"); present && !ok {
		out = append(out, "truncated is not a boolean")
	}
	if present, _ := res.is("cursor", "string"); present {
		if _, isStr := res.is("cursor", "string"); !isStr {
			if _, isNull := res.is("cursor", "null"); !isNull {
				out = append(out, "cursor is neither a string nor null")
			}
		}
	}
	if present, ok := res.is("deliveryStatus", "object"); present {
		if !ok {
			out = append(out, "deliveryStatus is not an object")
		} else {
			out = append(out, validateDeliveryStatus(res["deliveryStatus"])...)
		}
	}
	return out
}

// validateDeliveryStatus checks a deliveryStatus object.
func validateDeliveryStatus(raw json.RawMessage) []string {
	st, _ := parseObject(raw)
	var out []string
	if _, ok := st.is("active", "bool"); !ok {
		out = append(out, "deliveryStatus.active is not a boolean")
	}
	if present, _ := st.is("lastError", "string"); present {
		if _, isNull := st.is("lastError", "null"); !isNull {
			if !slices.Contains(mcpclient.LastErrorCategories, st.str("lastError")) {
				out = append(out, fmt.Sprintf("deliveryStatus.lastError %s is not one of %v", st["lastError"], mcpclient.LastErrorCategories))
			}
		}
	}
	for key, kind := range map[string]string{"throttled": "bool", "retryAfterMs": "integer"} {
		if present, ok := st.is(key, kind); present && !ok {
			if _, isNull := st.is(key, "null"); !isNull {
				out = append(out, fmt.Sprintf("deliveryStatus.%s is not a %s", key, kind))
			}
		}
	}
	return out
}

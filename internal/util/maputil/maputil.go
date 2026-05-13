package maputil

import (
	"encoding/json"
	"fmt"
)

// MergeStringMaps merges two string maps, with input taking precedence over original.
// This is useful for partial updates where we want to merge new values with existing ones.
func MergeStringMaps(original, input map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range original {
		merged[k] = v
	}
	for k, v := range input {
		merged[k] = v
	}
	return merged
}

// MergePatchStringMap applies RFC 7396 JSON merge-patch semantics to a map[string]string.
// The patch uses map[string]interface{} to preserve null values:
//   - nil value means delete the key
//   - non-nil value is converted to string and set
//
// Returns nil when all keys are removed (consistent with Go zero-value conventions).
func MergePatchStringMap(original map[string]string, patch map[string]any) map[string]string {
	merged := make(map[string]string)
	for k, v := range original {
		merged[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(merged, k)
		} else {
			merged[k] = toStringValue(v)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// toStringValue converts a JSON-deserialized value to a string,
// mirroring the coercion logic in MapStringString.UnmarshalJSON.
func toStringValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		return fmt.Sprintf("%v", val)
	case float64:
		return fmt.Sprintf("%v", val)
	default:
		if b, err := json.Marshal(val); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", val)
	}
}

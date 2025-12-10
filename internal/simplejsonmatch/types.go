// Package simplejsonmatch provides JSON schema matching functionality.
// It is a Go port of the simple-json-match TypeScript library.
// https://github.com/hookdeck/simple-json-match
package simplejsonmatch

// JSONType represents the type of a JSON value.
type JSONType string

const (
	JSONTypeNull      JSONType = "null"
	JSONTypeString    JSONType = "string"
	JSONTypeNumber    JSONType = "number"
	JSONTypeBoolean   JSONType = "boolean"
	JSONTypeObject    JSONType = "object"
	JSONTypeArray     JSONType = "array"
	JSONTypeUndefined JSONType = "undefined"
)

// Operator constants for schema matching.
const (
	OpEq         = "$eq"
	OpNeq        = "$neq"
	OpGt         = "$gt"
	OpGte        = "$gte"
	OpLt         = "$lt"
	OpLte        = "$lte"
	OpIn         = "$in"
	OpNin        = "$nin"
	OpStartsWith = "$startsWith"
	OpEndsWith   = "$endsWith"
	OpExist      = "$exist"
	OpOr         = "$or"
	OpAnd        = "$and"
	OpNot        = "$not"
	// OpRef = "$ref" // Not implemented - kept for reference
)

// operatorKeys is the list of all recognized operators (excluding logical operators).
var operatorKeys = map[string]bool{
	OpEq:         true,
	OpNeq:        true,
	OpGt:         true,
	OpGte:        true,
	OpLt:         true,
	OpLte:        true,
	OpIn:         true,
	OpNin:        true,
	OpStartsWith: true,
	OpEndsWith:   true,
	OpExist:      true,
}

// isOperatorKey returns true if the key is a recognized operator.
func isOperatorKey(key string) bool {
	return operatorKeys[key]
}

// getJSONType returns the JSON type of the given value.
func getJSONType(v any) JSONType {
	if v == nil {
		return JSONTypeNull
	}
	switch v.(type) {
	case string:
		return JSONTypeString
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return JSONTypeNumber
	case bool:
		return JSONTypeBoolean
	case []any, []string, []int, []float64, []map[string]any:
		return JSONTypeArray
	case map[string]any:
		return JSONTypeObject
	default:
		// Check for slice types using reflection-like approach
		switch v := v.(type) {
		case []any:
			return JSONTypeArray
		default:
			_ = v
			return JSONTypeObject
		}
	}
}

// isPrimitive returns true if the value is a primitive JSON type (null, string, number, boolean).
func isPrimitive(v any) bool {
	t := getJSONType(v)
	return t == JSONTypeNull || t == JSONTypeString || t == JSONTypeNumber || t == JSONTypeBoolean
}

// supportedType returns true if the value's type is in the list of supported types.
func supportedType(v any, types []JSONType) bool {
	t := getJSONType(v)
	for _, supported := range types {
		if t == supported {
			return true
		}
	}
	return false
}

// toFloat64 converts a numeric value to float64 for comparison.
// Returns the value and true if successful, 0 and false otherwise.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

// toSlice converts a value to []any if it's a slice type.
// Returns the slice and true if successful, nil and false otherwise.
func toSlice(v any) ([]any, bool) {
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		result := make([]any, len(s))
		for i, v := range s {
			result[i] = v
		}
		return result, true
	case []int:
		result := make([]any, len(s))
		for i, v := range s {
			result[i] = v
		}
		return result, true
	case []float64:
		result := make([]any, len(s))
		for i, v := range s {
			result[i] = v
		}
		return result, true
	case []map[string]any:
		result := make([]any, len(s))
		for i, v := range s {
			result[i] = v
		}
		return result, true
	default:
		return nil, false
	}
}

// toMap converts a value to map[string]any if it's a map type.
// Returns the map and true if successful, nil and false otherwise.
func toMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

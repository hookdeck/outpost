package simplejsonmatch

import (
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrUnsupportedType = errors.New("unsupported type for operator")
)

// opEq implements the $eq operator - deep equality check.
func opEq(v, compare any) (bool, error) {
	// For primitives, use direct comparison with type coercion
	if isPrimitive(v) && isPrimitive(compare) {
		return compareEquality(v, compare), nil
	}

	// For complex types, use JSON serialization for deep equality
	vJSON, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	compareJSON, err := json.Marshal(compare)
	if err != nil {
		return false, err
	}
	return string(vJSON) == string(compareJSON), nil
}

// compareEquality compares two primitive values with loose type coercion.
func compareEquality(v, compare any) bool {
	// Handle nil cases
	if v == nil && compare == nil {
		return true
	}
	if v == nil || compare == nil {
		return false
	}

	// Try numeric comparison
	vNum, vIsNum := toFloat64(v)
	cNum, cIsNum := toFloat64(compare)
	if vIsNum && cIsNum {
		return vNum == cNum
	}

	// Try string comparison
	vStr, vIsStr := v.(string)
	cStr, cIsStr := compare.(string)
	if vIsStr && cIsStr {
		return vStr == cStr
	}

	// Try bool comparison
	vBool, vIsBool := v.(bool)
	cBool, cIsBool := compare.(bool)
	if vIsBool && cIsBool {
		return vBool == cBool
	}

	return false
}

// opNeq implements the $neq operator - deep inequality check.
func opNeq(v, compare any) (bool, error) {
	eq, err := opEq(v, compare)
	return !eq, err
}

// opGt implements the $gt operator - greater than comparison.
func opGt(v, compare any) (bool, error) {
	// Check for string comparison
	vStr, vIsStr := v.(string)
	cStr, cIsStr := compare.(string)
	if vIsStr && cIsStr {
		return vStr > cStr, nil
	}

	// Check for numeric comparison
	vNum, vIsNum := toFloat64(v)
	cNum, cIsNum := toFloat64(compare)
	if vIsNum && cIsNum {
		return vNum > cNum, nil
	}

	return false, ErrUnsupportedType
}

// opGte implements the $gte operator - greater than or equal comparison.
func opGte(v, compare any) (bool, error) {
	// Check for string comparison
	vStr, vIsStr := v.(string)
	cStr, cIsStr := compare.(string)
	if vIsStr && cIsStr {
		return vStr >= cStr, nil
	}

	// Check for numeric comparison
	vNum, vIsNum := toFloat64(v)
	cNum, cIsNum := toFloat64(compare)
	if vIsNum && cIsNum {
		return vNum >= cNum, nil
	}

	return false, ErrUnsupportedType
}

// opLt implements the $lt operator - less than comparison.
func opLt(v, compare any) (bool, error) {
	// Check for string comparison
	vStr, vIsStr := v.(string)
	cStr, cIsStr := compare.(string)
	if vIsStr && cIsStr {
		return vStr < cStr, nil
	}

	// Check for numeric comparison
	vNum, vIsNum := toFloat64(v)
	cNum, cIsNum := toFloat64(compare)
	if vIsNum && cIsNum {
		return vNum < cNum, nil
	}

	return false, ErrUnsupportedType
}

// opLte implements the $lte operator - less than or equal comparison.
func opLte(v, compare any) (bool, error) {
	// Check for string comparison
	vStr, vIsStr := v.(string)
	cStr, cIsStr := compare.(string)
	if vIsStr && cIsStr {
		return vStr <= cStr, nil
	}

	// Check for numeric comparison
	vNum, vIsNum := toFloat64(v)
	cNum, cIsNum := toFloat64(compare)
	if vIsNum && cIsNum {
		return vNum <= cNum, nil
	}

	return false, ErrUnsupportedType
}

// opIn implements the $in operator - substring or array membership check.
func opIn(v, compare any) (bool, error) {
	// Case 1: compare is an array - check if v is in the array
	if compareSlice, ok := toSlice(compare); ok {
		// v must be a primitive that can be in an array
		if !supportedType(v, []JSONType{JSONTypeNumber, JSONTypeString, JSONTypeBoolean, JSONTypeNull}) {
			return false, ErrUnsupportedType
		}
		for _, item := range compareSlice {
			if compareEquality(v, item) {
				return true, nil
			}
		}
		return false, nil
	}

	// Case 2: compare is a string/number - check substring or array contains
	// If v is a string, check if compare is a substring
	if vStr, ok := v.(string); ok {
		if cStr, ok := compare.(string); ok {
			return strings.Contains(vStr, cStr), nil
		}
		return false, ErrUnsupportedType
	}

	// If v is an array, check if compare is in the array
	if vSlice, ok := toSlice(v); ok {
		for _, item := range vSlice {
			if compareEquality(item, compare) {
				return true, nil
			}
		}
		return false, nil
	}

	return false, ErrUnsupportedType
}

// opNin implements the $nin operator - negated membership check.
func opNin(v, compare any) (bool, error) {
	result, err := opIn(v, compare)
	return !result, err
}

// opStartsWith implements the $startsWith operator - prefix matching.
func opStartsWith(v, compare any) (bool, error) {
	vStr, ok := v.(string)
	if !ok {
		return false, ErrUnsupportedType
	}

	// compare can be a string or array of strings
	if cStr, ok := compare.(string); ok {
		return strings.HasPrefix(vStr, cStr), nil
	}

	if compareSlice, ok := toSlice(compare); ok {
		for _, item := range compareSlice {
			if cStr, ok := item.(string); ok {
				if strings.HasPrefix(vStr, cStr) {
					return true, nil
				}
			} else {
				return false, ErrUnsupportedType
			}
		}
		return false, nil
	}

	return false, ErrUnsupportedType
}

// opEndsWith implements the $endsWith operator - suffix matching.
func opEndsWith(v, compare any) (bool, error) {
	vStr, ok := v.(string)
	if !ok {
		return false, ErrUnsupportedType
	}

	// compare can be a string or array of strings
	if cStr, ok := compare.(string); ok {
		return strings.HasSuffix(vStr, cStr), nil
	}

	if compareSlice, ok := toSlice(compare); ok {
		for _, item := range compareSlice {
			if cStr, ok := item.(string); ok {
				if strings.HasSuffix(vStr, cStr) {
					return true, nil
				}
			} else {
				return false, ErrUnsupportedType
			}
		}
		return false, nil
	}

	return false, ErrUnsupportedType
}

// opExist implements the $exist operator - field presence check.
// Note: This is handled specially in the matching logic since it needs
// to know about field existence, not just the value.
// This function is called with v being the field value (or a special marker for undefined).
func opExist(v any, compare any) (bool, error) {
	cBool, ok := compare.(bool)
	if !ok {
		return false, ErrUnsupportedType
	}

	// v will be nil if the field exists with null value
	// v will be a special "undefined" marker if the field doesn't exist
	// We use the valueExists flag passed through context
	// For now, we check if v is the special undefined type
	_, isUndefined := v.(undefinedType)

	if cBool {
		// $exist: true - field must exist (value is not undefined)
		return !isUndefined, nil
	}
	// $exist: false - field must not exist (value is undefined)
	return isUndefined, nil
}

// undefinedType is a special marker type for undefined/missing fields.
type undefinedType struct{}

// undefined is the singleton value used to mark undefined fields.
var undefined = undefinedType{}

// applyOperator applies the given operator to the value and compare value.
func applyOperator(op string, v, compare any) (bool, error) {
	switch op {
	case OpEq:
		return opEq(v, compare)
	case OpNeq:
		return opNeq(v, compare)
	case OpGt:
		return opGt(v, compare)
	case OpGte:
		return opGte(v, compare)
	case OpLt:
		return opLt(v, compare)
	case OpLte:
		return opLte(v, compare)
	case OpIn:
		return opIn(v, compare)
	case OpNin:
		return opNin(v, compare)
	case OpStartsWith:
		return opStartsWith(v, compare)
	case OpEndsWith:
		return opEndsWith(v, compare)
	case OpExist:
		return opExist(v, compare)
	default:
		return false, errors.New("unknown operator: " + op)
	}
}

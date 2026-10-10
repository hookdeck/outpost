package topicschema

import (
	"encoding/json"
	"slices"
	"strings"
)

// maxRefDepth bounds $ref chains followed while inspecting a property schema.
const maxRefDepth = 32

var scalarTypes = map[string]bool{
	"string":  true,
	"number":  true,
	"integer": true,
	"boolean": true,
}

// filterableArgument reports whether prop, the schema of the top-level
// property name in the payload schema root, can be a subscription argument,
// and describes the argument. root and prop are trees from decodeJSON.
//
// A property is filterable when it is not hidden with x-mcp-filter: false,
// its name doesn't start with "$" (filters would read it as an operator), and
// its schema constrains it to scalar values: a type of string, number,
// integer or boolean (optionally nullable), or a scalar enum or const.
// Objects, arrays and composite schemas without a scalar type are not.
//
// Inference and the breaking-change diff both rely on this function, so a
// property's filterability is decided in exactly one place.
func filterableArgument(root any, name string, prop any) (Argument, bool) {
	if strings.HasPrefix(name, "$") {
		return Argument{}, false
	}
	if hiddenFromFilter(prop) {
		return Argument{}, false
	}
	node, ok := typedNode(root, prop)
	if !ok {
		return Argument{}, false
	}
	if hiddenFromFilter(node) {
		return Argument{}, false
	}

	arg := Argument{Name: name}
	var types []string
	switch t := node["type"].(type) {
	case string:
		types = []string{t}
	case []any:
		for _, v := range t {
			s, ok := v.(string)
			if !ok {
				return Argument{}, false
			}
			if s != "null" {
				types = append(types, s)
			}
		}
	case nil:
	default:
		return Argument{}, false
	}

	var enum []any
	if e, ok := node["enum"].([]any); ok {
		enum = e
	} else if c, ok := node["const"]; ok {
		enum = []any{c}
	} else if _, has := node["enum"]; has {
		// An enum that isn't an array is invalid; the compiler rejects it.
		return Argument{}, false
	}

	if len(types) == 0 {
		// No usable type: infer it from a scalar enum or const.
		if len(enum) == 0 {
			return Argument{}, false
		}
		for _, v := range enum {
			t := jsonTypeOf(v)
			if t == "null" {
				continue
			}
			if t == "" {
				return Argument{}, false
			}
			if !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
		if len(types) == 0 {
			return Argument{}, false
		}
	}
	for _, t := range types {
		if !scalarTypes[t] {
			return Argument{}, false
		}
	}
	slices.Sort(types)
	arg.Types = types

	for _, v := range enum {
		if v == nil {
			// null can't be filtered on as a value; skip it in the allowed set.
			continue
		}
		raw, err := marshalNoEscape(v)
		if err != nil {
			return Argument{}, false
		}
		arg.Enum = append(arg.Enum, json.RawMessage(raw))
	}

	if slices.Equal(types, []string{"string"}) {
		if f, _ := node["format"].(string); f == "date" || f == "date-time" {
			arg.Format = f
		}
	}
	// Filters compare strings bytewise, which orders fixed-width dates
	// correctly but not RFC 3339 timestamps with offsets or fractions, so
	// only numbers and dates get range operators.
	for _, t := range types {
		if t == "number" || t == "integer" {
			arg.Ranged = true
		}
	}
	if arg.Format == "date" {
		arg.Ranged = true
	}
	return arg, true
}

// propertyDescription returns the description of a property schema, looking
// through $ref chains when the property itself has none.
func propertyDescription(root any, prop any) string {
	cur := prop
	seen := map[string]bool{}
	for range maxRefDepth {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		if d, ok := m["description"].(string); ok {
			return d
		}
		ref, ok := m["$ref"].(string)
		if !ok || seen[ref] {
			return ""
		}
		seen[ref] = true
		next, err := resolveLocalRef(root, ref)
		if err != nil {
			return ""
		}
		cur = next
	}
	return ""
}

// hiddenFromFilter reports whether a schema node carries x-mcp-filter: false.
func hiddenFromFilter(node any) bool {
	m, ok := node.(map[string]any)
	if !ok {
		return false
	}
	v, ok := m["x-mcp-filter"].(bool)
	return ok && !v
}

// typedNode follows $ref chains from prop to the first schema object that
// declares type, enum or const. It reports false for boolean schemas,
// unresolvable or cyclic references, and schemas with none of the three.
func typedNode(root any, prop any) (map[string]any, bool) {
	cur := prop
	seen := map[string]bool{}
	for range maxRefDepth {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		_, hasType := m["type"]
		_, hasEnum := m["enum"]
		_, hasConst := m["const"]
		if hasType || hasEnum || hasConst {
			return m, true
		}
		ref, ok := m["$ref"].(string)
		if !ok || seen[ref] {
			return nil, false
		}
		seen[ref] = true
		next, err := resolveLocalRef(root, ref)
		if err != nil {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// jsonTypeOf returns the JSON Schema type name of a decodeJSON value, or ""
// for anything else.
func jsonTypeOf(v any) string {
	switch n := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		// 1.0 and 1e2 are integers in JSON Schema terms too; classifying
		// them as number only widens the argument type.
		if _, err := n.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	return ""
}

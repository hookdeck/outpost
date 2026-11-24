package topicschema

import "encoding/json"

// maxRefDepth bounds $ref chains followed while inspecting a property schema.
const maxRefDepth = 32

var scalarTypes = map[string]bool{
	"string":  true,
	"number":  true,
	"integer": true,
	"boolean": true,
}

// typedNode follows $ref chains from prop and merges the type, enum, const,
// default keywords found along the way, nearest first, so a
// keyword next to a $ref doesn't hide the ones behind it. It reports false
// for boolean schemas, unresolvable or cyclic references, and schemas where
// none of type, enum and const is declared.
func typedNode(root any, prop any) (map[string]any, bool) {
	cur := prop
	seen := map[string]bool{}
	merged := map[string]any{}
	for range maxRefDepth {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		for _, k := range typedKeywords {
			if v, has := m[k]; has {
				if _, set := merged[k]; !set {
					merged[k] = v
				}
			}
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return typedResult(merged)
		}
		if seen[ref] {
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

var typedKeywords = []string{"type", "enum", "const", "default"}

func typedResult(merged map[string]any) (map[string]any, bool) {
	_, hasType := merged["type"]
	_, hasEnum := merged["enum"]
	_, hasConst := merged["const"]
	if !hasType && !hasEnum && !hasConst {
		return nil, false
	}
	return merged, true
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

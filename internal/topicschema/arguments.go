package topicschema

import (
	"fmt"
	"slices"
	"strings"
)

// inputRootKeywords are the keywords accepted at the root of an inputSchema.
// Outpost matches each argument by equality with the payload field of the same
// name, so schemas that couldn't be mapped that way are refused.
var inputRootKeywords = map[string]bool{
	"$schema": true, "$comment": true, "$defs": true, "title": true,
	"description": true, "examples": true, "type": true, "properties": true,
	"required": true, "additionalProperties": true, "minProperties": true,
	"maxProperties": true,
}

// derivedProperty is one property of a derived inputSchema.
type derivedProperty struct {
	Type        any    `json:"type"`
	Description string `json:"description,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
}

// derivedInputSchema is the inputSchema of an event that declares none.
type derivedInputSchema struct {
	Type                 string                     `json:"type"`
	Properties           map[string]derivedProperty `json:"properties"`
	AdditionalProperties bool                       `json:"additionalProperties"`
}

// deriveInputSchema returns the inputSchema of an event without one: every
// top-level payload property that is a string, number, integer or boolean
// becomes an optional argument carrying the property's type, scalar enum and
// description. Other properties are left out. payload is a tree from
// decodeJSON.
func deriveInputSchema(payload map[string]any) ([]byte, error) {
	schema := derivedInputSchema{Type: "object", Properties: map[string]derivedProperty{}}
	props, _ := payload["properties"].(map[string]any)
	for _, name := range sortedKeys(props) {
		// Arguments become a filter, where "$" keys are operators.
		if strings.HasPrefix(name, "$") {
			continue
		}
		node, ok := typedNode(payload, props[name])
		if !ok {
			continue
		}
		types, ok := scalarArgumentTypes(node, true)
		if !ok {
			continue
		}
		var prop derivedProperty
		prop.Type = types[0]
		if len(types) > 1 {
			prop.Type = types
		}
		if p, ok := props[name].(map[string]any); ok {
			prop.Description, _ = p["description"].(string)
		}
		prop.Enum = scalarEnum(node, types)
		schema.Properties[name] = prop
	}
	return marshalNoEscape(schema)
}

// scalarEnum returns the enum of a typedNode when every value besides null is
// of one of types, and nil otherwise.
func scalarEnum(node map[string]any, types []string) []any {
	enum, _ := node["enum"].([]any)
	var out []any
	for _, v := range enum {
		t := jsonTypeOf(v)
		if t == "null" {
			continue
		}
		if !compatibleTypes([]string{t}, types) {
			return nil
		}
		out = append(out, v)
	}
	return out
}

// checkInputSchema checks a compiled inputSchema against the payload schema
// and describes its arguments. Both are trees from decodeJSON. It returns the
// problems found, which are worded without the event prefix.
func checkInputSchema(input, payload map[string]any) ([]Argument, []string) {
	var problems []string
	for _, kw := range sortedKeys(input) {
		if !inputRootKeywords[kw] {
			problems = append(problems, fmt.Sprintf("inputSchema keyword %q is not supported; list each argument under properties", clip(kw)))
		}
	}
	if v, ok := input["additionalProperties"]; ok && v != false {
		problems = append(problems, "inputSchema additionalProperties must be false or absent; Outpost accepts only the arguments listed in properties")
	}
	props, _ := input["properties"].(map[string]any)
	required, _ := input["required"].([]any)
	for _, r := range required {
		if name, ok := r.(string); ok {
			if _, listed := props[name]; !listed {
				problems = append(problems, fmt.Sprintf("inputSchema required lists %s, which is not under properties", quoteJSONString(clip(name))))
			}
		}
	}
	payloadProps, _ := payload["properties"].(map[string]any)
	var args []Argument
	for _, name := range sortedKeys(props) {
		arg, msg := checkArgument(name, input, props[name], payload, payloadProps)
		if msg != "" {
			problems = append(problems, msg)
			continue
		}
		args = append(args, arg)
	}
	return args, problems
}

func checkArgument(name string, input map[string]any, prop any, payload map[string]any, payloadProps map[string]any) (Argument, string) {
	q := "argument " + quoteJSONString(clip(name))
	// Arguments become a filter, where "$" keys are operators.
	if strings.HasPrefix(name, "$") {
		return Argument{}, q + `: names can't start with "$"`
	}
	node, ok := typedNode(input, prop)
	var types []string
	if ok {
		types, ok = scalarArgumentTypes(node, false)
	}
	if !ok {
		return Argument{}, q + " must be a string, number, integer or boolean; arguments match a payload field by equality"
	}
	if _, has := node["default"]; has {
		return Argument{}, q + " has a default, which Outpost doesn't apply; an omitted argument doesn't filter"
	}
	pprop, ok := payloadProps[name]
	if !ok {
		return Argument{}, q + fmt.Sprintf(" has no top-level property %s in payloadSchema; each argument matches the payload field of the same name", quoteJSONString(clip(name)))
	}
	var ptypes []string
	pnode, ok := typedNode(payload, pprop)
	if ok {
		ptypes, ok = scalarArgumentTypes(pnode, true)
	}
	if !ok {
		return Argument{}, q + fmt.Sprintf(": payloadSchema property %s is not a string, number, integer or boolean", quoteJSONString(clip(name)))
	}
	if !compatibleTypes(types, ptypes) {
		return Argument{}, q + fmt.Sprintf(" is %s but payloadSchema property %s is %s", strings.Join(types, " or "), quoteJSONString(clip(name)), strings.Join(ptypes, " or "))
	}
	return Argument{Name: name, Types: types}, ""
}

// scalarArgumentTypes returns the sorted scalar types of a typedNode, from
// type, else from a scalar enum or const. null is ignored when allowNull (a
// nullable payload field) and refused otherwise.
func scalarArgumentTypes(node map[string]any, allowNull bool) ([]string, bool) {
	var types []string
	switch t := node["type"].(type) {
	case string:
		types = []string{t}
	case []any:
		for _, v := range t {
			s, ok := v.(string)
			if !ok {
				return nil, false
			}
			types = append(types, s)
		}
	case nil:
		var enum []any
		if e, ok := node["enum"].([]any); ok {
			enum = e
		} else if c, ok := node["const"]; ok {
			enum = []any{c}
		}
		for _, v := range enum {
			t := jsonTypeOf(v)
			if t == "" {
				return nil, false
			}
			if !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
	default:
		return nil, false
	}
	if !allowNull && slices.Contains(types, "null") {
		return nil, false
	}
	types = slices.DeleteFunc(types, func(t string) bool { return t == "null" })
	if len(types) == 0 {
		return nil, false
	}
	for _, t := range types {
		if !scalarTypes[t] {
			return nil, false
		}
	}
	slices.Sort(types)
	return slices.Compact(types), true
}

// compatibleTypes reports whether every argument type can match a payload
// type; integer and number count as the same.
func compatibleTypes(arg, payload []string) bool {
	numeric := func(t string) bool { return t == "integer" || t == "number" }
	for _, a := range arg {
		if !slices.ContainsFunc(payload, func(p string) bool { return p == a || numeric(a) && numeric(p) }) {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

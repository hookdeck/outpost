package topicschema

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Inferred argument limits. They bound the filters a subscription can store
// and the work each delivery spends matching them.
const (
	maxArgumentStringLength = 256
	maxArgumentListItems    = 100
)

// rangeOperators are the operator object keys accepted by ranged arguments,
// in the order they appear in the inputSchema.
var rangeOperators = []string{"$gt", "$gte", "$lt", "$lte"}

// Keywords whose values are schemas, by shape. Walks over payload schemas
// descend only through these, so values of const, enum, default, examples
// and unknown keywords are never mistaken for schemas.
var (
	subschemaKeywords = map[string]bool{
		"additionalItems":       true,
		"additionalProperties":  true,
		"contains":              true,
		"contentSchema":         true,
		"else":                  true,
		"if":                    true,
		"items":                 true,
		"not":                   true,
		"propertyNames":         true,
		"then":                  true,
		"unevaluatedItems":      true,
		"unevaluatedProperties": true,
	}
	subschemaListKeywords = map[string]bool{
		"allOf":       true,
		"anyOf":       true,
		"oneOf":       true,
		"prefixItems": true,
	}
	// Values of these are objects mapping names to schemas. "dependencies"
	// also maps to arrays of names, which are skipped.
	subschemaMapKeywords = map[string]bool{
		"$defs":             true,
		"definitions":       true,
		"dependencies":      true,
		"dependentSchemas":  true,
		"patternProperties": true,
		"properties":        true,
	}
)

// visitSchemaObjects calls fn for every schema object in node, a tree from
// decodeJSON, with its dot path from root. Boolean schemas are skipped.
func visitSchemaObjects(node any, path string, fn func(obj map[string]any, path string)) {
	visitSchemas(node, path, func(schema any, path string) {
		if obj, ok := schema.(map[string]any); ok {
			fn(obj, path)
		}
	})
}

// visitSchemas calls fn for every schema in node, a tree from decodeJSON,
// boolean schemas included, with its dot path from root.
func visitSchemas(node any, path string, fn func(schema any, path string)) {
	obj, ok := node.(map[string]any)
	if !ok {
		if _, ok := node.(bool); ok {
			fn(node, path)
		}
		return
	}
	fn(obj, path)
	for key, value := range obj {
		switch {
		case subschemaKeywords[key]:
			// items may still be an array in schemas written for older drafts;
			// the metaschema rejects it, but walking it is harmless.
			if list, ok := value.([]any); ok {
				for i, v := range list {
					visitSchemas(v, indexPath(appendPathKey(path, key), i), fn)
				}
				continue
			}
			visitSchemas(value, appendPathKey(path, key), fn)
		case subschemaListKeywords[key]:
			list, _ := value.([]any)
			for i, v := range list {
				visitSchemas(v, indexPath(appendPathKey(path, key), i), fn)
			}
		case subschemaMapKeywords[key]:
			m, _ := value.(map[string]any)
			for name, v := range m {
				visitSchemas(v, appendPathKey(appendPathKey(path, key), name), fn)
			}
		}
	}
}

// propertyNameSet returns every name declared under properties anywhere in
// the schema tree. Error paths show instance keys only when they are in it,
// since those strings come from the schema rather than from the instance.
func propertyNameSet(tree any) map[string]struct{} {
	names := map[string]struct{}{}
	visitSchemaObjects(tree, "", func(obj map[string]any, _ string) {
		if props, ok := obj["properties"].(map[string]any); ok {
			for name := range props {
				names[name] = struct{}{}
			}
		}
	})
	return names
}

// stripMCPFilter returns the schema without x-mcp-filter in any schema
// object, keeping member order. raw must be compact and free of duplicate
// keys; the result is compact.
func stripMCPFilter(raw json.RawMessage) (json.RawMessage, error) {
	if !isJSONObject(raw) {
		// A boolean schema.
		return raw, nil
	}
	members, err := objectMembers(raw)
	if err != nil {
		return nil, err
	}
	out := make(orderedObject, 0, len(members))
	for _, m := range members {
		value := m.Value
		switch {
		case m.Key == "x-mcp-filter":
			continue
		case subschemaKeywords[m.Key]:
			if bytes.HasPrefix(value, []byte("[")) {
				value, err = mapJSONArray(value, stripMCPFilter)
			} else {
				value, err = stripMCPFilter(value)
			}
		case subschemaListKeywords[m.Key]:
			value, err = mapJSONArray(value, stripMCPFilter)
		case subschemaMapKeywords[m.Key]:
			value, err = mapJSONObject(value, func(v json.RawMessage) (json.RawMessage, error) {
				if bytes.HasPrefix(v, []byte("[")) {
					// A dependencies entry listing property names.
					return v, nil
				}
				return stripMCPFilter(v)
			})
		}
		if err != nil {
			return nil, err
		}
		out = append(out, member{Key: m.Key, Value: value})
	}
	return out.MarshalJSON()
}

// mapJSONArray applies fn to every element of a JSON array. Values that
// aren't arrays are returned unchanged.
func mapJSONArray(raw json.RawMessage, fn func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw, nil
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		v, err := fn(item)
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// mapJSONObject applies fn to every member value of a JSON object, keeping
// member order. Values that aren't objects are returned unchanged.
func mapJSONObject(raw json.RawMessage, fn func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	if !isJSONObject(raw) {
		return raw, nil
	}
	members, err := objectMembers(raw)
	if err != nil {
		return nil, err
	}
	for i, m := range members {
		v, err := fn(m.Value)
		if err != nil {
			return nil, err
		}
		members[i].Value = v
	}
	return members.MarshalJSON()
}

func isJSONObject(raw []byte) bool {
	raw = bytes.TrimLeft(raw, " \t\r\n")
	return len(raw) > 0 && raw[0] == '{'
}

// inferArguments returns the filterable top-level properties of a payload
// schema, in document order, and the inputSchema accepting them. raw is the
// compact schema and tree its decodeJSON form; the root has type object.
//
// Each argument accepts a value, a list of values (an $in filter), and for
// ranged arguments an operator object. Strings are capped at 256 characters
// and lists at 100 items, so stored filters stay small. Without withEnums,
// the inputSchema leaves out the arguments' enums.
func inferArguments(raw json.RawMessage, tree map[string]any, withEnums bool) ([]Argument, json.RawMessage, error) {
	properties := orderedObject{}
	var args []Argument
	root, err := objectMembers(raw)
	if err != nil {
		return nil, nil, err
	}
	props, _ := tree["properties"].(map[string]any)
	if propsRaw := root.get("properties"); propsRaw != nil && props != nil {
		members, err := objectMembers(propsRaw)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range members {
			prop := props[m.Key]
			arg, ok := filterableArgument(tree, m.Key, prop)
			if !ok {
				continue
			}
			schema, err := argumentSchema(arg, propertyDescription(tree, prop), propertyMaxLength(tree, prop), withEnums)
			if err != nil {
				return nil, nil, err
			}
			args = append(args, arg)
			properties = append(properties, member{Key: m.Key, Value: schema})
		}
	}
	propertiesJSON, err := properties.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}
	input, err := orderedObject{
		{Key: "type", Value: json.RawMessage(`"object"`)},
		{Key: "properties", Value: propertiesJSON},
		{Key: "additionalProperties", Value: json.RawMessage(`false`)},
	}.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}
	return args, input, nil
}

// argumentSchema returns the inputSchema entry for arg: anyOf a value, a list
// of values and, for ranged arguments, an operator object. maxLength is the
// property's own maxLength, or -1 when it has none. withEnum keeps the enum
// of values and list items.
func argumentSchema(arg Argument, description string, maxLength int, withEnum bool) (json.RawMessage, error) {
	value, err := argumentValueSchema(arg, maxLength, withEnum)
	if err != nil {
		return nil, err
	}
	list, err := orderedObject{
		{Key: "type", Value: json.RawMessage(`"array"`)},
		{Key: "items", Value: value},
		{Key: "minItems", Value: json.RawMessage(`1`)},
		{Key: "maxItems", Value: json.RawMessage(strconv.Itoa(maxArgumentListItems))},
	}.MarshalJSON()
	if err != nil {
		return nil, err
	}
	branches := []json.RawMessage{value, list}
	if arg.Ranged {
		operand, err := argumentValueSchema(arg, maxLength, false)
		if err != nil {
			return nil, err
		}
		ops := make(orderedObject, 0, len(rangeOperators))
		for _, op := range rangeOperators {
			ops = append(ops, member{Key: op, Value: operand})
		}
		opsJSON, err := ops.MarshalJSON()
		if err != nil {
			return nil, err
		}
		object, err := orderedObject{
			{Key: "type", Value: json.RawMessage(`"object"`)},
			{Key: "properties", Value: opsJSON},
			{Key: "additionalProperties", Value: json.RawMessage(`false`)},
			{Key: "minProperties", Value: json.RawMessage(`1`)},
		}.MarshalJSON()
		if err != nil {
			return nil, err
		}
		branches = append(branches, object)
	}
	anyOf, err := marshalNoEscape(branches)
	if err != nil {
		return nil, err
	}
	var out orderedObject
	if description != "" {
		d, err := marshalNoEscape(description)
		if err != nil {
			return nil, err
		}
		out = append(out, member{Key: "description", Value: d})
	}
	out = append(out, member{Key: "anyOf", Value: anyOf})
	return out.MarshalJSON()
}

// argumentValueSchema returns the schema of one argument value. withEnum is
// false for range operator operands, which compare rather than match.
func argumentValueSchema(arg Argument, maxLength int, withEnum bool) (json.RawMessage, error) {
	var typ any = arg.Types[0]
	if len(arg.Types) > 1 {
		typ = arg.Types
	}
	t, err := marshalNoEscape(typ)
	if err != nil {
		return nil, err
	}
	out := orderedObject{{Key: "type", Value: t}}
	if withEnum && arg.Enum != nil {
		enum, err := marshalNoEscape(arg.Enum)
		if err != nil {
			return nil, err
		}
		out = append(out, member{Key: "enum", Value: enum})
	}
	if arg.Format != "" {
		f, err := marshalNoEscape(arg.Format)
		if err != nil {
			return nil, err
		}
		out = append(out, member{Key: "format", Value: f})
	}
	for _, typ := range arg.Types {
		if typ == "string" {
			limit := maxArgumentStringLength
			if maxLength >= 0 && maxLength < limit {
				limit = maxLength
			}
			out = append(out, member{Key: "maxLength", Value: json.RawMessage(strconv.Itoa(limit))})
			break
		}
	}
	return out.MarshalJSON()
}

// propertyMaxLength returns the smallest maxLength along the $ref chain of a
// property schema, since every schema on the chain applies, or -1.
func propertyMaxLength(root any, prop any) int {
	limit := -1
	cur := prop
	seen := map[string]bool{}
	for range maxRefDepth {
		m, ok := cur.(map[string]any)
		if !ok {
			break
		}
		if n, ok := m["maxLength"].(json.Number); ok {
			if v, err := n.Int64(); err == nil && v >= 0 && (limit < 0 || v < int64(limit)) {
				limit = int(min(v, int64(maxArgumentStringLength)))
			}
		}
		ref, ok := m["$ref"].(string)
		if !ok || seen[ref] {
			break
		}
		seen[ref] = true
		next, err := resolveLocalRef(root, ref)
		if err != nil {
			break
		}
		cur = next
	}
	return limit
}

// mcpDescription is the events/list description: the topic description,
// prefixed for deprecated topics so agents pick the replacement.
func mcpDescription(t Topic) string {
	if !t.Deprecated {
		return t.Description
	}
	prefix := "Deprecated."
	if t.ReplacedBy != "" {
		prefix = "Deprecated: use " + t.ReplacedBy + "."
	}
	if t.Description == "" {
		return prefix
	}
	return prefix + " " + t.Description
}

// buildMCPEvent precomputes the events/list entry of an MCP-enabled topic.
// raw is the compact payload schema and tree its decodeJSON form.
func buildMCPEvent(t Topic, raw json.RawMessage, tree map[string]any) (*MCPEvent, []Argument, error) {
	args, input, err := inferArguments(raw, tree, true)
	if err != nil {
		return nil, nil, err
	}
	payload, err := stripMCPFilter(raw)
	if err != nil {
		return nil, nil, err
	}
	ev := &MCPEvent{Name: t.Name, Description: mcpDescription(t)}
	name, err := marshalNoEscape(ev.Name)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(`{"name":`)
	buf.Write(name)
	if ev.Description != "" {
		d, err := marshalNoEscape(ev.Description)
		if err != nil {
			return nil, nil, err
		}
		buf.WriteString(`,"description":`)
		buf.Write(d)
	}
	buf.WriteString(`,"delivery":["webhook"],"inputSchema":`)
	inputStart := buf.Len()
	buf.Write(input)
	inputEnd := buf.Len()
	buf.WriteString(`,"payloadSchema":`)
	payloadStart := buf.Len()
	buf.Write(payload)
	payloadEnd := buf.Len()
	buf.WriteByte('}')

	// InputSchema and PayloadSchema share JSON's memory rather than holding
	// another copy of each schema. The capacity limits keep an append to one
	// from writing into JSON.
	ev.JSON = bytes.Clone(buf.Bytes())
	ev.InputSchema = ev.JSON[inputStart:inputEnd:inputEnd]
	ev.PayloadSchema = ev.JSON[payloadStart:payloadEnd:payloadEnd]
	return ev, args, nil
}

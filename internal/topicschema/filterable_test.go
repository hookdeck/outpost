package topicschema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterableArgument(t *testing.T) {
	root, err := decodeJSON([]byte(`{
		"type": "object",
		"$defs": {
			"currency": {"type": "string", "enum": ["USD", "EUR"], "description": "ISO code"},
			"loop": {"$ref": "#/$defs/loop"}
		},
		"properties": {
			"total": {"type": "number", "description": "Total"},
			"count": {"type": ["integer", "null"]},
			"currency": {"$ref": "#/$defs/currency"},
			"status": {"enum": ["open", "closed", null]},
			"kind": {"const": "order"},
			"day": {"type": "string", "format": "date"},
			"at": {"type": "string", "format": "date-time"},
			"id": {"type": "string", "x-mcp-filter": false},
			"hiddenRef": {"$ref": "#/$defs/currency", "x-mcp-filter": false},
			"items": {"type": "array", "items": {"type": "string"}},
			"meta": {"type": "object"},
			"mixed": {"type": ["string", "object"]},
			"any": {},
			"loop": {"$ref": "#/$defs/loop"},
			"$op": {"type": "string"}
		}
	}`))
	require.NoError(t, err)
	props := root.(map[string]any)["properties"].(map[string]any)

	cases := []struct {
		name   string
		ok     bool
		types  []string
		enum   []string
		format string
		ranged bool
	}{
		{name: "total", ok: true, types: []string{"number"}, ranged: true},
		{name: "count", ok: true, types: []string{"integer"}, ranged: true},
		{name: "currency", ok: true, types: []string{"string"}, enum: []string{`"USD"`, `"EUR"`}},
		{name: "status", ok: true, types: []string{"string"}, enum: []string{`"open"`, `"closed"`}},
		{name: "kind", ok: true, types: []string{"string"}, enum: []string{`"order"`}},
		{name: "day", ok: true, types: []string{"string"}, format: "date", ranged: true},
		{name: "at", ok: true, types: []string{"string"}, format: "date-time"},
		{name: "id"},
		{name: "hiddenRef"},
		{name: "items"},
		{name: "meta"},
		{name: "mixed"},
		{name: "any"},
		{name: "loop"},
		{name: "$op"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arg, ok := filterableArgument(root, tc.name, props[tc.name])
			require.Equal(t, tc.ok, ok)
			if !ok {
				return
			}
			assert.Equal(t, tc.name, arg.Name)
			assert.Equal(t, tc.types, arg.Types)
			var enum []string
			for _, e := range arg.Enum {
				enum = append(enum, string(e))
			}
			assert.Equal(t, tc.enum, enum)
			assert.Equal(t, tc.format, arg.Format)
			assert.Equal(t, tc.ranged, arg.Ranged)
		})
	}

	assert.Equal(t, "ISO code", propertyDescription(root, props["currency"]))
	assert.Equal(t, "Total", propertyDescription(root, props["total"]))
	assert.Equal(t, "", propertyDescription(root, props["loop"]))
}

func TestMixedTypeArgumentHasNoRangeOperators(t *testing.T) {
	c := eventCatalog(t, `{"type":"object","properties":{
		"mixed":{"type":["string","integer"]},
		"num":{"type":["number","null"]}
	}}`)
	args := c.byName["t"].args
	require.Len(t, args, 2)
	assert.False(t, args[0].Ranged)
	assert.True(t, args[1].Ranged)

	assert.NotEmpty(t, c.ValidateArguments("t", []byte(`{"mixed":{"$gt":"abc"}}`)))
	assert.Nil(t, c.ValidateArguments("t", []byte(`{"num":{"$gt":1}}`)))
}

func TestTypedNodeMergesKeywordsAcrossRef(t *testing.T) {
	root, err := decodeJSON([]byte(`{
		"$defs": {"day": {"type": "string", "format": "date"}, "code": {"enum": ["a", "b"]}},
		"properties": {
			"day": {"$ref": "#/$defs/day", "description": "d"},
			"code": {"type": "string", "$ref": "#/$defs/code"},
			"hidden": {"type": "string", "$ref": "#/$defs/day", "x-mcp-filter": false}
		}
	}`))
	require.NoError(t, err)
	props := root.(map[string]any)["properties"].(map[string]any)

	day, ok := filterableArgument(root, "day", props["day"])
	require.True(t, ok)
	assert.Equal(t, "date", day.Format)
	assert.True(t, day.Ranged)

	code, ok := filterableArgument(root, "code", props["code"])
	require.True(t, ok)
	assert.Equal(t, []string{"string"}, code.Types)
	assert.Len(t, code.Enum, 2)

	_, ok = filterableArgument(root, "hidden", props["hidden"])
	assert.False(t, ok)
}

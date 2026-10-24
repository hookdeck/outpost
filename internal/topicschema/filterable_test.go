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

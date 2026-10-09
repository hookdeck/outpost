package topicschema

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readCompact reads a JSON test file and compacts it, keeping member order.
func readCompact(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, json.Compact(&buf, raw))
	return buf.Bytes()
}

// orderCreatedCatalog builds a catalog from the spec's order.created topic.
func orderCreatedCatalog(t *testing.T) *Catalog {
	t.Helper()
	var def Definition
	require.NoError(t, json.Unmarshal(readCompact(t, "testdata/catalog/order_created.topic.json"), &def))
	c, err := NewCatalog([]string{"order.created"}, Definitions{"order.created": def})
	require.NoError(t, err)
	return c
}

func TestMCPEventGolden(t *testing.T) {
	c := orderCreatedCatalog(t)
	ev, ok := c.MCPEvent("order.created")
	require.True(t, ok)

	// Compared byte for byte: member order is part of the output, and
	// payloadSchema keeps the configured property order.
	want := readCompact(t, "testdata/catalog/order_created.event.json")
	assert.Equal(t, string(want), string(ev.JSON))

	var entry struct {
		InputSchema   json.RawMessage `json:"inputSchema"`
		PayloadSchema json.RawMessage `json:"payloadSchema"`
	}
	require.NoError(t, json.Unmarshal(want, &entry))
	assert.Equal(t, "order.created", ev.Name)
	assert.Equal(t, "Fires when a new order is placed.", ev.Description)
	assert.Equal(t, string(entry.InputSchema), string(ev.InputSchema))
	assert.Equal(t, string(entry.PayloadSchema), string(ev.PayloadSchema))
}

// The spec's events/list example leaves out limits the design adds to the
// inferred inputSchema. Removing exactly those gives the spec's example:
//   - strings carry maxLength (256, or the property's own when smaller);
//   - every argument, numbers included, accepts a list of values, and lists
//     carry minItems 1 and maxItems 100;
//   - operator objects carry minProperties 1.
func TestInputSchemaMatchesSpecExampleUpToDocumentedLimits(t *testing.T) {
	c := orderCreatedCatalog(t)
	ev, ok := c.MCPEvent("order.created")
	require.True(t, ok)

	var input map[string]any
	require.NoError(t, json.Unmarshal(ev.InputSchema, &input))
	for name, prop := range input["properties"].(map[string]any) {
		var kept []any
		for _, b := range prop.(map[string]any)["anyOf"].([]any) {
			branch := b.(map[string]any)
			delete(branch, "maxLength")
			delete(branch, "minProperties")
			if branch["type"] == "array" {
				if name == "total" {
					continue
				}
				delete(branch, "minItems")
				delete(branch, "maxItems")
				delete(branch["items"].(map[string]any), "maxLength")
			}
			kept = append(kept, branch)
		}
		prop.(map[string]any)["anyOf"] = kept
	}
	got, err := json.Marshal(input)
	require.NoError(t, err)
	assert.JSONEq(t, string(readCompact(t, "testdata/catalog/order_created.spec_input_schema.json")), string(got))
}

func TestInferArguments(t *testing.T) {
	const schema = `{
		"type": "object",
		"$defs": {
			"currency": {"type": "string", "enum": ["USD", "EUR"], "description": "ISO code"},
			"code": {"$ref": "#/$defs/shortCode", "maxLength": 20},
			"shortCode": {"type": "string", "maxLength": 8},
			"hidden": {"type": "string", "x-mcp-filter": false}
		},
		"properties": {
			"total": {"type": "number"},
			"count": {"type": ["integer", "null"], "description": "Item count"},
			"currency": {"$ref": "#/$defs/currency"},
			"status": {"enum": ["open", "closed", null]},
			"kind": {"const": "order"},
			"mixed": {"enum": ["a", 1]},
			"flag": {"type": "boolean"},
			"day": {"type": "string", "format": "date"},
			"at": {"type": "string", "format": "date-time"},
			"email": {"type": "string", "format": "email"},
			"code": {"$ref": "#/$defs/code"},
			"long": {"type": "string", "maxLength": 300},
			"tiny": {"type": "string", "maxLength": 4},
			"id": {"type": "string", "x-mcp-filter": false},
			"hiddenByRef": {"$ref": "#/$defs/hidden"},
			"$op": {"type": "string"},
			"tags": {"type": "array", "items": {"type": "string"}},
			"meta": {"type": "object"},
			"anything": {}
		}
	}`
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(schema), MCP: MCPSettings{Enabled: true}}})
	require.NoError(t, err)

	list := func(item string) string {
		return `{"type":"array","items":` + item + `,"minItems":1,"maxItems":100}`
	}
	ops := func(operand string) string {
		return `{"type":"object","properties":{"$gt":` + operand + `,"$gte":` + operand + `,"$lt":` + operand + `,"$lte":` + operand + `},"additionalProperties":false,"minProperties":1}`
	}
	anyOf := func(description string, branches ...string) string {
		out := `{`
		if description != "" {
			out += `"description":"` + description + `",`
		}
		out += `"anyOf":[`
		for i, b := range branches {
			if i > 0 {
				out += ","
			}
			out += b
		}
		return out + `]}`
	}
	const (
		number     = `{"type":"number"}`
		integer    = `{"type":"integer"}`
		str        = `{"type":"string","maxLength":256}`
		currency   = `{"type":"string","enum":["USD","EUR"],"maxLength":256}`
		status     = `{"type":"string","enum":["open","closed"],"maxLength":256}`
		kind       = `{"type":"string","enum":["order"],"maxLength":256}`
		mixed      = `{"type":["integer","string"],"enum":["a",1],"maxLength":256}`
		mixedRange = `{"type":["integer","string"],"maxLength":256}`
		boolean    = `{"type":"boolean"}`
		date       = `{"type":"string","format":"date","maxLength":256}`
		dateTime   = `{"type":"string","format":"date-time","maxLength":256}`
		code       = `{"type":"string","maxLength":8}`
		tiny       = `{"type":"string","maxLength":4}`
	)
	want := []struct {
		name   string
		schema string
		arg    Argument
	}{
		{"total", anyOf("", number, list(number), ops(number)), Argument{Types: []string{"number"}, Ranged: true}},
		{"count", anyOf("Item count", integer, list(integer), ops(integer)), Argument{Types: []string{"integer"}, Ranged: true}},
		{"currency", anyOf("ISO code", currency, list(currency)), Argument{Types: []string{"string"}, Enum: raws(`"USD"`, `"EUR"`)}},
		{"status", anyOf("", status, list(status)), Argument{Types: []string{"string"}, Enum: raws(`"open"`, `"closed"`)}},
		{"kind", anyOf("", kind, list(kind)), Argument{Types: []string{"string"}, Enum: raws(`"order"`)}},
		{"mixed", anyOf("", mixed, list(mixed), ops(mixedRange)), Argument{Types: []string{"integer", "string"}, Enum: raws(`"a"`, `1`), Ranged: true}},
		{"flag", anyOf("", boolean, list(boolean)), Argument{Types: []string{"boolean"}}},
		// Dates compare correctly as strings, so they get range operators.
		{"day", anyOf("", date, list(date), ops(date)), Argument{Types: []string{"string"}, Format: "date", Ranged: true}},
		// Timestamps with offsets or fractions don't, so they don't.
		{"at", anyOf("", dateTime, list(dateTime)), Argument{Types: []string{"string"}, Format: "date-time"}},
		{"email", anyOf("", str, list(str)), Argument{Types: []string{"string"}}},
		// The smallest maxLength along the $ref chain applies.
		{"code", anyOf("", code, list(code)), Argument{Types: []string{"string"}}},
		{"long", anyOf("", str, list(str)), Argument{Types: []string{"string"}}},
		{"tiny", anyOf("", tiny, list(tiny)), Argument{Types: []string{"string"}}},
	}

	ev, ok := c.MCPEvent("t")
	require.True(t, ok)
	var input struct {
		Type                 string          `json:"type"`
		Properties           json.RawMessage `json:"properties"`
		AdditionalProperties bool            `json:"additionalProperties"`
	}
	require.NoError(t, json.Unmarshal(ev.InputSchema, &input))
	assert.Equal(t, "object", input.Type)
	assert.False(t, input.AdditionalProperties)
	props, err := objectMembers(input.Properties)
	require.NoError(t, err)

	args := c.Arguments("t")
	require.Len(t, props, len(want))
	require.Len(t, args, len(want))
	for i, w := range want {
		t.Run(w.name, func(t *testing.T) {
			assert.Equal(t, w.name, props[i].Key, "document order")
			assert.JSONEq(t, w.schema, string(props[i].Value))
			w.arg.Name = w.name
			assert.Equal(t, w.arg, args[i])
		})
	}
}

func raws(values ...string) []json.RawMessage {
	out := make([]json.RawMessage, len(values))
	for i, v := range values {
		out[i] = json.RawMessage(v)
	}
	return out
}

func TestInferArgumentsWithoutProperties(t *testing.T) {
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {
		PayloadSchema: json.RawMessage(`{"type":"object","additionalProperties":true}`),
		MCP:           MCPSettings{Enabled: true},
	}})
	require.NoError(t, err)
	ev, ok := c.MCPEvent("t")
	require.True(t, ok)
	assert.Equal(t, `{"type":"object","properties":{},"additionalProperties":false}`, string(ev.InputSchema))
	assert.Empty(t, c.Arguments("t"))
	assert.Nil(t, c.ValidateArguments("t", []byte(`{}`)))
	assert.Equal(t, []string{"arguments: has properties that are not allowed"}, c.ValidateArguments("t", []byte(`{"a":1}`)))
}

func TestMCPDescription(t *testing.T) {
	object := json.RawMessage(`{"type":"object"}`)
	mcp := MCPSettings{Enabled: true}
	c, err := NewCatalog([]string{"old", "bare", "silent", "plain", "nodesc", "order.created.v2"}, Definitions{
		"old":              {Description: "Fires on orders.", PayloadSchema: object, MCP: mcp, Deprecated: true, ReplacedBy: "order.created.v2"},
		"bare":             {Description: "Fires on orders.", PayloadSchema: object, MCP: mcp, Deprecated: true},
		"silent":           {PayloadSchema: object, MCP: mcp, Deprecated: true, ReplacedBy: "order.created.v2"},
		"plain":            {Description: "Fires on orders.", PayloadSchema: object, MCP: mcp},
		"nodesc":           {PayloadSchema: object, MCP: mcp},
		"order.created.v2": {PayloadSchema: object, MCP: mcp},
	})
	require.NoError(t, err)

	for name, want := range map[string]string{
		"old":    "Deprecated: use order.created.v2. Fires on orders.",
		"bare":   "Deprecated. Fires on orders.",
		"silent": "Deprecated: use order.created.v2.",
		"plain":  "Fires on orders.",
		"nodesc": "",
	} {
		ev, ok := c.MCPEvent(name)
		require.True(t, ok, name)
		assert.Equal(t, want, ev.Description, name)
		topic, _ := c.Topic(name)
		assert.NotContains(t, topic.Description, "Deprecated", "the topic keeps its own description")
	}
	ev, _ := c.MCPEvent("nodesc")
	assert.Equal(t, `{"name":"nodesc","delivery":["webhook"],"inputSchema":{"type":"object","properties":{},"additionalProperties":false},"payloadSchema":{"type":"object"}}`, string(ev.JSON))
}

func TestStripMCPFilter(t *testing.T) {
	in := `{"type":"object","x-mcp-filter":true,` +
		`"properties":{` +
		`"x-mcp-filter":{"type":"string","x-mcp-filter":false},` +
		`"a":{"type":"object","properties":{"b":{"x-mcp-filter":false,"type":"string"}}},` +
		`"c":{"type":"array","items":{"x-mcp-filter":false},"prefixItems":[{"x-mcp-filter":false}],"contains":{"x-mcp-filter":false}},` +
		`"d":{"allOf":[{"x-mcp-filter":false}],"anyOf":[true,{"x-mcp-filter":false}],"oneOf":[{"x-mcp-filter":false}],"not":{"x-mcp-filter":false},` +
		`"const":{"x-mcp-filter":false},"enum":[{"x-mcp-filter":false}],"default":{"x-mcp-filter":false},"examples":[{"x-mcp-filter":false}]},` +
		`"e":{"additionalProperties":{"x-mcp-filter":false},"patternProperties":{"^x":{"x-mcp-filter":false}},"propertyNames":{"x-mcp-filter":false}},` +
		`"f":{"if":{"x-mcp-filter":false},"then":{"x-mcp-filter":false},"else":{"x-mcp-filter":false},"dependentSchemas":{"g":{"x-mcp-filter":false}},"dependencies":{"z":["a"],"y":{"x-mcp-filter":false}}}` +
		`},` +
		`"$defs":{"g":{"x-mcp-filter":false,"type":"string","unevaluatedProperties":{"x-mcp-filter":false}}},` +
		`"x-other":{"x-mcp-filter":false}}`
	want := `{"type":"object",` +
		`"properties":{` +
		`"x-mcp-filter":{"type":"string"},` +
		`"a":{"type":"object","properties":{"b":{"type":"string"}}},` +
		`"c":{"type":"array","items":{},"prefixItems":[{}],"contains":{}},` +
		`"d":{"allOf":[{}],"anyOf":[true,{}],"oneOf":[{}],"not":{},` +
		`"const":{"x-mcp-filter":false},"enum":[{"x-mcp-filter":false}],"default":{"x-mcp-filter":false},"examples":[{"x-mcp-filter":false}]},` +
		`"e":{"additionalProperties":{},"patternProperties":{"^x":{}},"propertyNames":{}},` +
		`"f":{"if":{},"then":{},"else":{},"dependentSchemas":{"g":{}},"dependencies":{"z":["a"],"y":{}}}` +
		`},` +
		`"$defs":{"g":{"type":"string","unevaluatedProperties":{}}},` +
		`"x-other":{"x-mcp-filter":false}}`
	got, err := stripMCPFilter(json.RawMessage(in))
	require.NoError(t, err)
	assert.Equal(t, want, string(got))

	got, err = stripMCPFilter(json.RawMessage(`true`))
	require.NoError(t, err)
	assert.Equal(t, `true`, string(got))
}

func TestPayloadSchemaKeepsTextAsWritten(t *testing.T) {
	// Escapes, number forms and HTML characters survive compaction and the
	// x-mcp-filter strip unchanged.
	schema := `{"type": "object", "description": "a <b> & é", "properties": {"n": {"type": "number", "maximum": 1.50, "x-mcp-filter": false}}}`
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(schema), MCP: MCPSettings{Enabled: true}}})
	require.NoError(t, err)
	ev, _ := c.MCPEvent("t")
	assert.Equal(t, `{"type":"object","description":"a <b> & é","properties":{"n":{"type":"number","maximum":1.50}}}`, string(ev.PayloadSchema))
	topic, _ := c.Topic("t")
	assert.Equal(t, `{"type":"object","description":"a <b> & é","properties":{"n":{"type":"number","maximum":1.50,"x-mcp-filter":false}}}`, string(topic.PayloadSchema))
}

package topicschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func def(name, input, payload string) Definition {
	d := Definition{Name: name}
	if input != "" {
		d.InputSchema = json.RawMessage(input)
	}
	if payload != "" {
		d.PayloadSchema = json.RawMessage(payload)
	}
	return d
}

func configProblems(t *testing.T, err error) []string {
	t.Helper()
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr), "want *ConfigError, got %T: %v", err, err)
	return cfgErr.Problems
}

const orderPayload = `{"type":"object","properties":{
	"status":{"type":"string","enum":["paid","pending"]},
	"total":{"type":"number"},
	"count":{"type":["integer","null"]},
	"tags":{"type":"array"},
	"deep":{"$ref":"#/$defs/flag"},
	"id":{"type":"string"}},"$defs":{"flag":{"type":"boolean"}}}`

func TestNewCatalogRules(t *testing.T) {
	re2 := "error parsing regexp: invalid or unsupported Perl syntax: `(?=`"
	in := func(props string) string { return `{"type":"object","properties":{` + props + `}}` }
	cases := []struct {
		name   string
		topics []string
		defs   Definitions
		want   []string // exact problems, sorted; empty = valid
	}{
		{name: "no arguments", defs: Definitions{def("a", emptyInput, objPayload)}},
		{name: "enum argument", defs: Definitions{def("a", in(`"status":{"type":"string","enum":["paid"]}`), orderPayload)}},
		{name: "integer argument on number field", defs: Definitions{def("a", in(`"total":{"type":"integer"}`), orderPayload)}},
		{name: "number argument on integer field", defs: Definitions{def("a", in(`"count":{"type":"number"}`), orderPayload)}},
		{name: "argument through ref", defs: Definitions{def("a", `{"type":"object","properties":{"deep":{"$ref":"#/$defs/b"}},"$defs":{"b":{"type":"boolean"}},"required":["deep"]}`, orderPayload)}},
		{name: "scalar enum without type", defs: Definitions{def("a", in(`"id":{"enum":["x","y"]}`), orderPayload)}},
		{name: "additionalProperties false", defs: Definitions{def("a", `{"type":"object","properties":{},"additionalProperties":false,"title":"t"}`, objPayload)}},
		{
			name:   "not in TOPICS",
			topics: []string{"a"},
			defs:   Definitions{def("b", emptyInput, objPayload)},
			want:   []string{`event "b" is not in TOPICS`},
		},
		{
			name:   "TOPICS whitespace",
			topics: []string{" a"},
			defs:   Definitions{def("a", emptyInput, objPayload)},
			want:   []string{`event "a" is not in TOPICS (did you mean " a"?)`},
		},
		{
			name: "wildcard",
			defs: Definitions{def("user.*", emptyInput, objPayload)},
			want: []string{`event "user.*": wildcard topics can't have event definitions`},
		},
		{
			name: "duplicate",
			defs: Definitions{def("a", emptyInput, objPayload), def("b", emptyInput, objPayload), def("a", emptyInput, objPayload)},
			want: []string{`events[2]: duplicate name "a", already defined at events[0]`},
		},
		{
			name: "required schemas",
			defs: Definitions{def("a", "", ""), def("b", "", objPayload), def("c", emptyInput, "")},
			want: []string{
				`event "a": inputSchema is required; use {"type": "object", "properties": {}} for an event without arguments`,
				`event "a": payloadSchema is required`,
				`event "b": inputSchema is required; use {"type": "object", "properties": {}} for an event without arguments`,
				`event "c": payloadSchema is required`,
			},
		},
		{
			name: "schema shape",
			defs: Definitions{def("a", `[]`, `"x"`), def("b", `{"properties":{}}`, `{}`)},
			want: []string{
				`event "a": inputSchema must be a JSON object`,
				`event "a": payloadSchema must be a JSON object`,
				`event "b": inputSchema must have "type": "object"`,
				`event "b": payloadSchema must have "type": "object"`,
			},
		},
		{
			name: "size",
			defs: Definitions{def("a", `{"type":"object","description":"`+strings.Repeat("x", 70000)+`"}`, objPayload)},
			want: []string{`event "a": inputSchema is 70034 bytes; the limit is 65536`},
		},
		{
			name: "unsupported root keywords",
			defs: Definitions{def("a", `{"type":"object","properties":{},"anyOf":[{}],"patternProperties":{},"$ref":"#/$defs/x","$defs":{"x":{}},"x-note":1}`, objPayload)},
			want: []string{
				`event "a": inputSchema keyword "$ref" is not supported; list each argument under properties`,
				`event "a": inputSchema keyword "anyOf" is not supported; list each argument under properties`,
				`event "a": inputSchema keyword "patternProperties" is not supported; list each argument under properties`,
				`event "a": inputSchema keyword "x-note" is not supported; list each argument under properties`,
			},
		},
		{
			name: "additionalProperties true",
			defs: Definitions{def("a", `{"type":"object","properties":{},"additionalProperties":true}`, objPayload)},
			want: []string{`event "a": inputSchema additionalProperties must be false or absent; Outpost accepts only the arguments listed in properties`},
		},
		{
			name: "dollar name",
			defs: Definitions{def("a", in(`"$gt":{"type":"number"}`), `{"type":"object","properties":{"$gt":{"type":"number"}}}`)},
			want: []string{`event "a": argument "$gt": names can't start with "$"`},
		},
		{
			name: "non-scalar arguments",
			defs: Definitions{def("a", in(`"tags":{"type":"array"},"id":{"type":["string","null"]},"x":{},"y":{"type":"object"}`), orderPayload)},
			want: []string{
				`event "a": argument "id" must be a string, number, integer or boolean; arguments match a payload field by equality`,
				`event "a": argument "tags" must be a string, number, integer or boolean; arguments match a payload field by equality`,
				`event "a": argument "x" must be a string, number, integer or boolean; arguments match a payload field by equality`,
				`event "a": argument "y" must be a string, number, integer or boolean; arguments match a payload field by equality`,
			},
		},
		{
			name: "not in payload",
			defs: Definitions{def("a", in(`"from":{"type":"string"}`), orderPayload)},
			want: []string{`event "a": argument "from" has no top-level property "from" in payloadSchema; each argument matches the payload field of the same name`},
		},
		{
			name: "payload property not scalar",
			defs: Definitions{def("a", in(`"tags":{"type":"string"}`), orderPayload)},
			want: []string{`event "a": argument "tags": payloadSchema property "tags" is not a string, number, integer or boolean`},
		},
		{
			name: "type mismatch",
			defs: Definitions{def("a", in(`"total":{"type":"string"},"deep":{"type":"integer"}`), orderPayload)},
			want: []string{
				`event "a": argument "deep" is integer but payloadSchema property "deep" is boolean`,
				`event "a": argument "total" is string but payloadSchema property "total" is number`,
			},
		},
		{
			name: "default",
			defs: Definitions{def("a", in(`"status":{"type":"string","default":"paid"}`), orderPayload)},
			want: []string{`event "a": argument "status" has a default, which Outpost doesn't apply; an omitted argument doesn't filter`},
		},
		{
			name: "unsupported pattern",
			defs: Definitions{def("a", in(`"id":{"type":"string","pattern":"(?=x)"}`), orderPayload)},
			want: []string{`event "a": inputSchema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2},
		},
		{
			name: "payload compile",
			defs: Definitions{def("a", emptyInput, `{"type":"object","properties":{"x":{"pattern":"(?=x)"}}}`)},
			want: []string{`event "a": payloadSchema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2},
		},
		{
			name: "nested id",
			defs: Definitions{def("a", `{"type":"object","properties":{"s":{"type":"string","$id":"x"}}}`, `{"type":"object","properties":{"s":{"type":"string"}}}`)},
			want: []string{`event "a": inputSchema.properties.s.$id is only allowed at the inputSchema root, since it changes how references below it resolve`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			catalog, err := NewCatalog(c.topics, c.defs)
			if len(c.want) == 0 {
				require.NoError(t, err)
				require.NotNil(t, catalog)
				return
			}
			got := configProblems(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestCatalogOrderAndIdentity(t *testing.T) {
	defs := Definitions{
		def("z.last", `{"type":"object","properties":{"status":{"type":"string","enum":["paid","pending"]}}}`, orderPayload),
		def("a.first", emptyInput, `{"type":"object","x-vendor":{"b":1,"a":2},"properties":{"b":{},"a":{}}}`),
	}
	defs[0].Description = "Orders."
	defs[0].Meta = json.RawMessage(`{"owner":"billing"}`)
	defs[1].Delivery = []string{"webhook", "poll"}
	c, err := NewCatalog(nil, defs)
	require.NoError(t, err)

	events := c.Events()
	require.Len(t, events, 2)
	assert.Equal(t, "z.last", events[0].Name, "configured order")
	assert.Equal(t, "a.first", events[1].Name)
	assert.Equal(t, `{"type":"object","x-vendor":{"b":1,"a":2},"properties":{"b":{},"a":{}}}`, string(events[1].PayloadSchema), "verbatim")
	assert.Contains(t, string(events[1].JSON), `"delivery":["webhook"]`)
	assert.Contains(t, string(events[0].JSON), `"_meta":{"owner":"billing"}`)

	// An exported list loads back as the same value.
	var list []json.RawMessage
	for _, ev := range events {
		list = append(list, ev.JSON)
	}
	doc, err := json.Marshal(map[string]any{"events": list})
	require.NoError(t, err)
	again, err := ParseDefinitionsJSON(doc)
	require.NoError(t, err)
	c2, err := NewCatalog(nil, again)
	require.NoError(t, err)
	doc2, err := json.Marshal(map[string]any{"events": eventJSONs(c2)})
	require.NoError(t, err)
	assert.JSONEq(t, string(doc), string(doc2))
	assert.Equal(t, string(doc), string(doc2))
}

func eventJSONs(c *Catalog) []json.RawMessage {
	var out []json.RawMessage
	for _, ev := range c.Events() {
		out = append(out, ev.JSON)
	}
	return out
}

func TestEventGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/catalog/order_created.topic.json")
	require.NoError(t, err)
	defs, err := ParseDefinitionsJSON([]byte(`{"events":[` + string(raw) + `]}`))
	require.NoError(t, err)
	c, err := NewCatalog([]string{"order.created"}, defs)
	require.NoError(t, err)
	ev, ok := c.Event("order.created")
	require.True(t, ok)
	want, err := os.ReadFile("testdata/catalog/order_created.event.json")
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(ev.JSON))
	assert.Equal(t, "Fires when a new order is placed.", ev.Description)
}

func TestSpecExampleList(t *testing.T) {
	// Arguments of the proposal's examples that Outpost can't map fail with
	// the expected texts; the rest of the shape is accepted.
	doc := `{"resultType":"complete","events":[{
		"name":"email.received","description":"New mail",
		"inputSchema":{"type":"object","properties":{"from":{"type":"string"},"redact_pii":{"type":"boolean"}}},
		"payloadSchema":{"type":"object","properties":{"subject":{"type":"string"}}},
		"delivery":["push","poll","webhook"]}]}`
	defs, err := ParseDefinitionsJSON([]byte(doc))
	require.NoError(t, err)
	_, err = NewCatalog(nil, defs)
	assert.Equal(t, []string{
		`event "email.received": argument "from" has no top-level property "from" in payloadSchema; each argument matches the payload field of the same name`,
		`event "email.received": argument "redact_pii" has no top-level property "redact_pii" in payloadSchema; each argument matches the payload field of the same name`,
	}, configProblems(t, err))
}

func TestCatalogAccessors(t *testing.T) {
	c, err := NewCatalog(nil, Definitions{def("a", emptyInput, objPayload)})
	require.NoError(t, err)
	_, ok := c.Event("a")
	assert.True(t, ok)
	_, ok = c.Event("b")
	assert.False(t, ok)

	var nilCatalog *Catalog
	assert.Nil(t, nilCatalog.Events())
	_, ok = nilCatalog.Event("a")
	assert.False(t, ok)
	assert.NotEmpty(t, nilCatalog.ValidateArguments("a", nil))
}

func TestNewCatalogBoundsProblems(t *testing.T) {
	var defs Definitions
	for i := range 150 {
		defs = append(defs, def(fmt.Sprintf("e%03d", i), "", ""))
	}
	_, err := NewCatalog(nil, defs)
	problems := configProblems(t, err)
	assert.Len(t, problems, maxReportedProblems+1)
	assert.Equal(t, "... and 200 more", problems[len(problems)-1])
}

func TestValidateArguments(t *testing.T) {
	c, err := NewCatalog(nil, Definitions{
		def("order", `{"type":"object","properties":{
			"status":{"type":"string","enum":["paid","pending"]},
			"total":{"type":"number","minimum":0},
			"deep":{"type":"boolean"}}}`, orderPayload),
		def("plain", emptyInput, orderPayload),
		def("strict", `{"type":"object","properties":{"id":{"type":"string"}},"additionalProperties":false,"required":["id"]}`, orderPayload),
	})
	require.NoError(t, err)

	valid := []struct{ event, args string }{
		{"order", ""}, {"order", `{}`}, {"order", `{"status":"paid"}`}, {"order", `{"status":"pending","total":10,"deep":true}`},
		{"order", `{"total":1.50}`}, {"plain", `{}`}, {"strict", `{"id":"x"}`},
	}
	for _, v := range valid {
		assert.Empty(t, c.ValidateArguments(v.event, []byte(v.args)), "%s %s", v.event, v.args)
	}

	invalid := []struct{ name, event, args, want string }{
		{"enum miss", "order", `{"status":"refunded"}`, "arguments.status: " + enumMessageFor},
		{"wrong type", "order", `{"total":"10"}`, "arguments.total: "},
		{"list value", "order", `{"status":["paid"]}`, "arguments.status: "},
		{"operator object", "order", `{"total":{"$gt":1}}`, "arguments.total: "},
		{"below minimum", "order", `{"total":-1}`, "arguments.total: "},
		{"not an argument", "order", `{"id":"x"}`, "arguments.*: is not an argument of this event"},
		{"not an argument on a no-argument event", "plain", `{"status":"paid"}`, "arguments.*: is not an argument of this event"},
		{"not an argument, strict", "strict", `{"id":"x","other":1}`, "arguments"},
		{"missing required", "strict", `{}`, "arguments: "},
		{"not an object", "order", `[]`, "arguments: "},
		{"not json", "order", `{`, "arguments: must be valid JSON"},
		{"large number", "order", `{"total":1e5000}`, "arguments.total: " + largeNumberMessage},
		{"too large", "order", `{"status":"` + strings.Repeat("x", 5000) + `"}`, "arguments: exceed the size limit"},
		{"undefined", "nope", `{}`, "arguments: event is not defined"},
	}
	for _, v := range invalid {
		t.Run(v.name, func(t *testing.T) {
			errs := c.ValidateArguments(v.event, []byte(v.args))
			require.NotEmpty(t, errs)
			assert.True(t, strings.HasPrefix(errs[0], v.want), "%q", errs)
			for _, e := range errs {
				assert.NotContains(t, e, "refunded")
				assert.NotContains(t, e, `"other"`)
			}
		})
	}
}

const enumMessageFor = "must be one of"

func TestCatalogConcurrentUse(t *testing.T) {
	c, err := NewCatalog(nil, Definitions{def("order", `{"type":"object","properties":{"status":{"type":"string"}}}`, orderPayload)})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				assert.Empty(t, c.ValidateArguments("order", []byte(`{"status":"paid"}`)))
				assert.NotEmpty(t, c.ValidateArguments("order", []byte(`{"status":1}`)))
			}
		}()
	}
	wg.Wait()
}

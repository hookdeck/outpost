package topicschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func schemaDef(schema string) Definition {
	return Definition{PayloadSchema: json.RawMessage(schema)}
}

func configProblems(t *testing.T, err error) []string {
	t.Helper()
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr), "want *ConfigError, got %T: %v", err, err)
	return cfgErr.Problems
}

func TestNewCatalogRules(t *testing.T) {
	object := `{"type":"object"}`
	mcp := MCPSettings{Enabled: true}
	with := func(def Definition, edit func(*Definition)) Definition {
		edit(&def)
		return def
	}
	re2 := "error parsing regexp: invalid or unsupported Perl syntax: `(?=`"

	cases := []struct {
		name     string
		topics   []string
		defs     Definitions
		imported Definitions
		want     []string
	}{
		// Keys and TOPICS.
		{
			name:   "key not in TOPICS",
			topics: []string{"a"},
			defs:   Definitions{"b": {Description: "x"}},
			want:   []string{`topic "b" is not in TOPICS`},
		},
		{
			name:   "key differs from a TOPICS entry by whitespace",
			topics: []string{" a", "b"},
			defs:   Definitions{"a": {}, "b ": {}},
			want: []string{
				`topic "a" is not in TOPICS (did you mean " a"?)`,
				`topic "b " is not in TOPICS (did you mean "b"?)`,
			},
		},
		{
			name:   "wildcard keys",
			topics: []string{"*", "user.*"},
			defs:   Definitions{"*": {}, "user.*": {}, "order.*": {}},
			want: []string{
				`topic "*": wildcard topics can't have schemas`,
				`topic "order.*": wildcard topics can't have schemas`,
				`topic "user.*": wildcard topics can't have schemas`,
			},
		},
		{
			name: "empty TOPICS",
			defs: Definitions{"a": {}},
			want: []string{"topic schemas require TOPICS to list the topics"},
		},

		// Topic fields.
		{
			name:   "name differs from key",
			topics: []string{"a"},
			defs:   Definitions{"a": {Name: "b"}},
			want:   []string{`topic "a": name "b" must match the topic key`},
		},
		// payload_schema shape and size.
		{
			name:   "payload_schema not an object",
			topics: []string{"a", "b", "c"},
			defs:   Definitions{"a": schemaDef(`[]`), "b": schemaDef(`true`), "c": schemaDef(`"x"`)},
			want: []string{
				`topic "a": payload_schema must be a JSON object`,
				`topic "b": payload_schema must be a JSON object`,
				`topic "c": payload_schema must be a JSON object`,
			},
		},
		{
			name:   "payload_schema not JSON",
			topics: []string{"a"},
			defs:   Definitions{"a": schemaDef(`{"type":`)},
			want:   []string{`topic "a": payload_schema is not valid JSON: unexpected end of JSON input`},
		},
		{
			name:   "payload_schema too large",
			topics: []string{"a"},
			defs:   Definitions{"a": schemaDef(`{"description":"` + strings.Repeat("x", 256<<10) + `"}`)},
			want:   []string{fmt.Sprintf(`topic "a": payload_schema is %d bytes; the limit is 262144`, 256<<10+18)},
		},
		{
			name:   "MCP payload_schema too large",
			topics: []string{"a"},
			defs: Definitions{"a": with(schemaDef(`{"type":"object","description":"`+strings.Repeat("x", 64<<10)+`"}`),
				func(d *Definition) { d.MCP = mcp })},
			want: []string{fmt.Sprintf(`topic "a": payload_schema is %d bytes; the limit for MCP-enabled topics is 65536`, 64<<10+34)},
		},
		{
			name:   "size is measured compact",
			topics: []string{"a"},
			defs:   Definitions{"a": schemaDef(`{"type":"object"` + strings.Repeat(" ", 300<<10) + `}`)},
		},
		{
			name:   "duplicate keys",
			topics: []string{"a", "b"},
			defs: Definitions{
				"a": schemaDef(`{"type":"object","type":"array"}`),
				"b": schemaDef(`{"properties":{"x":{"type":"string","const":{"k":1,"k":2}}}}`),
			},
			want: []string{
				`topic "a": payload_schema has a duplicate key "type"`,
				`topic "b": payload_schema.properties.x.const has a duplicate key "k"`,
			},
		},

		// Compiling.
		{
			name:   "metaschema violations",
			topics: []string{"a", "b", "c"},
			defs: Definitions{
				"a": schemaDef(`{"properties":{"total":{"minimum":"10"}}}`),
				"b": schemaDef(`{"type":"objekt"}`),
				// Annotation keywords are still checked with the format
				// vocabulary registered.
				"c": schemaDef(`{"$defs":{"x":{"description":5}}}`),
			},
			want: []string{
				`topic "a": payload_schema.properties.total.minimum: must be number`,
				`topic "b": payload_schema.type: must be array`,
				`topic "b": payload_schema.type: must be one of the allowed values`,
				`topic "c": payload_schema.$defs.x.description: must be string`,
			},
		},
		{
			name:   "unresolvable local references",
			topics: []string{"a"},
			defs: Definitions{
				"a": schemaDef(`{"properties":{"x":{"$ref":"#/$defs/missing"}}}`),
			},
			want: []string{
				`topic "a": payload_schema: json-pointer in "#/$defs/missing" not found`,
			},
		},
		{
			// The validator resolves anchors, but inference and the
			// breaking-change diff follow JSON pointers only.
			name:   "anchor references",
			topics: []string{"a", "b", "c", "d", "ok"},
			defs: Definitions{
				"a":  schemaDef(`{"$anchor":"root","properties":{"x":{"$ref":"#root"}}}`),
				"b":  schemaDef(`{"$ref":"#nowhere"}`),
				"c":  schemaDef(`{"$dynamicAnchor":"meta","properties":{"x":{"$dynamicRef":"#meta"}}}`),
				"d":  schemaDef(`{"properties":{"x":{"$ref":"#%zz"}}}`),
				"ok": schemaDef(`{"properties":{"x":{"$ref":"#"},"y":{"$ref":"#%2Fproperties%2Fx"},"z":{"$dynamicRef":"#/properties/x"}}}`),
			},
			want: []string{
				`topic "a": payload_schema.properties.x.$ref "#root" must be a JSON pointer such as "#/$defs/name"; anchors are not supported`,
				`topic "b": payload_schema.$ref "#nowhere" must be a JSON pointer such as "#/$defs/name"; anchors are not supported`,
				`topic "c": payload_schema.properties.x.$dynamicRef "#meta" must be a JSON pointer such as "#/$defs/name"; anchors are not supported`,
				`topic "d": payload_schema.properties.x.$ref "#%zz" must be a JSON pointer such as "#/$defs/name"; anchors are not supported`,
			},
		},
		{
			// A nested $id starts a resource that the validator resolves the
			// references below it against, unlike inference and the diff.
			name:   "$id and anchors below the root",
			topics: []string{"a", "b", "c", "root"},
			defs: Definitions{
				"a": schemaDef(`{"type":"object","$defs":{"code":{"type":"string"}},
					"properties":{"code":{"$id":"https://example.com/code","$ref":"#/$defs/code","$defs":{"code":{"type":"integer"}}}}}`),
				"b":    schemaDef(`{"$defs":{"n":{"$anchor":"node","type":"string"}}}`),
				"c":    schemaDef(`{"items":{"$dynamicAnchor":"meta"}}`),
				"root": schemaDef(`{"$id":"https://example.com/order","$anchor":"order","$dynamicAnchor":"meta","type":"object","properties":{"x":{"$ref":"#"}}}`),
			},
			want: []string{
				`topic "a": payload_schema.properties.code.$id is only allowed at the payload_schema root, since it changes how references below it resolve`,
				`topic "b": payload_schema.$defs.n.$anchor is only allowed at the payload_schema root; reference subschemas with JSON pointers such as "#/$defs/name"`,
				`topic "c": payload_schema.items.$dynamicAnchor is only allowed at the payload_schema root; reference subschemas with JSON pointers such as "#/$defs/name"`,
			},
		},
		{
			// The validator compiles whatever a reference points to as a
			// schema, so one into an extension or a default would bring in
			// $id, anchors and references the checks above never see.
			name:   "references to values that aren't schemas",
			topics: []string{"a", "b", "c", "d", "ok"},
			defs: Definitions{
				"a": schemaDef(`{"type":"object","properties":{"a":{"$ref":"#/x-lib/A"}},
					"x-lib":{"A":{"$id":"https://other.invalid/a.json","$defs":{"s":{"type":"string"}},"$ref":"#/$defs/s"}},
					"$defs":{"s":{"type":"integer"}}}`),
				"b": schemaDef(`{"$anchor":"root","properties":{"a":{"$ref":"#/x-lib/A/properties/b"}},
					"x-lib":{"A":{"properties":{"b":{"$ref":"#root"}}}}}`),
				"c": schemaDef(`{"properties":{"a":{"default":{"type":"string"}},"b":{"$dynamicRef":"#/properties/a/default"}}}`),
				"d": schemaDef(`{"$defs":{"s":{"type":"string"}},"items":{"$ref":"#/$defs"}}`),
				"ok": schemaDef(`{"type":"object","allOf":[{"type":"object"}],
					"properties":{"a":{"$ref":"#/$defs/t"},"b":{"$ref":"#/allOf/0"},"c":{"$ref":"#/$defs/a~1b"},"d":{"$ref":"#/$defs/a%7E1b/not"},"e":{"$ref":"#"}},
					"$defs":{"t":true,"a/b":{"not":{"type":"null"}}}}`),
			},
			want: []string{
				`topic "a": payload_schema.properties.a.$ref "#/x-lib/A" must point to a schema, such as "#/$defs/name", not into another value such as an extension or a default`,
				`topic "b": payload_schema.properties.a.$ref "#/x-lib/A/properties/b" must point to a schema, such as "#/$defs/name", not into another value such as an extension or a default`,
				`topic "c": payload_schema.properties.b.$dynamicRef "#/properties/a/default" must point to a schema, such as "#/$defs/name", not into another value such as an extension or a default`,
				`topic "d": payload_schema.items.$ref "#/$defs" must point to a schema, such as "#/$defs/name", not into another value such as an extension or a default`,
			},
		},
		{
			name:   "external references",
			topics: []string{"a", "b", "c", "d", "e", "f"},
			defs: Definitions{
				"a": schemaDef(`{"$ref":"file:///etc/passwd"}`),
				"b": schemaDef(`{"properties":{"x":{"$ref":"http://example.com/schema.json"}}}`),
				"c": schemaDef(`{"$defs":{"x":{"$ref":"https://example.com/schema.json#/$defs/y"}}}`),
				"d": schemaDef(`{"items":{"$ref":"other.json"}}`),
				"e": schemaDef(`{"$dynamicRef":"https://example.com/meta#meta"}`),
				"f": schemaDef(`{"$ref":"https://json-schema.org/draft/2020-12/schema"}`),
			},
			want: []string{
				`topic "a": payload_schema.$ref "file:///etc/passwd" is not a local reference; only references starting with # are allowed`,
				`topic "b": payload_schema.properties.x.$ref "http://example.com/schema.json" is not a local reference; only references starting with # are allowed`,
				`topic "c": payload_schema.$defs.x.$ref "https://example.com/schema.json#/$defs/y" is not a local reference; only references starting with # are allowed`,
				`topic "d": payload_schema.items.$ref "other.json" is not a local reference; only references starting with # are allowed`,
				`topic "e": payload_schema.$dynamicRef "https://example.com/meta#meta" is not a local reference; only references starting with # are allowed`,
				`topic "f": payload_schema.$ref "https://json-schema.org/draft/2020-12/schema" is not a local reference; only references starting with # are allowed`,
			},
		},
		{
			name:   "$schema other than 2020-12",
			topics: []string{"a", "b", "c"},
			defs: Definitions{
				"a": schemaDef(`{"$schema":"http://json-schema.org/draft-07/schema#"}`),
				"b": schemaDef(`{"$schema":"https://json-schema.org/draft/2020-12/schema#"}`),
				"c": schemaDef(`{"$defs":{"x":{"$id":"https://example.com/x","$schema":"https://json-schema.org/draft/2019-09/schema"}}}`),
			},
			want: []string{
				`topic "a": payload_schema.$schema must be "https://json-schema.org/draft/2020-12/schema"`,
				`topic "b": payload_schema.$schema must be "https://json-schema.org/draft/2020-12/schema"`,
				`topic "c": payload_schema.$defs.x.$id is only allowed at the payload_schema root, since it changes how references below it resolve`,
				`topic "c": payload_schema.$defs.x.$schema must be "https://json-schema.org/draft/2020-12/schema"`,
			},
		},
		{
			name:   "patterns RE2 can't compile in MCP-enabled topics",
			topics: []string{"mcp", "props"},
			defs: Definitions{
				"mcp":   with(schemaDef(`{"type":"object","properties":{"a":{"pattern":"(?=x)"}}}`), func(d *Definition) { d.MCP = mcp }),
				"props": with(schemaDef(`{"type":"object","patternProperties":{"(?=x)":true}}`), func(d *Definition) { d.MCP = mcp }),
			},
			want: []string{
				`topic "mcp": payload_schema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2,
				`topic "props": payload_schema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2,
			},
		},

		// MCP.
		{
			name:   "mcp.enabled without payload_schema",
			topics: []string{"a"},
			defs:   Definitions{"a": {MCP: mcp}},
			want:   []string{`topic "a": mcp.enabled requires payload_schema`},
		},
		{
			name:   "mcp.enabled with a root that isn't an object",
			topics: []string{"a", "b", "c"},
			defs: Definitions{
				"a": with(schemaDef(`{"type":"array"}`), func(d *Definition) { d.MCP = mcp }),
				"b": with(schemaDef(`{"properties":{}}`), func(d *Definition) { d.MCP = mcp }),
				"c": with(schemaDef(`{"type":["object"]}`), func(d *Definition) { d.MCP = mcp }),
			},
			want: []string{
				`topic "a": mcp.enabled requires a payload_schema with "type": "object"`,
				`topic "b": mcp.enabled requires a payload_schema with "type": "object"`,
				`topic "c": mcp.enabled requires a payload_schema with "type": "object"`,
			},
		},
		{
			name:   "x-mcp-filter not a boolean",
			topics: []string{"a"},
			defs: Definitions{"a": schemaDef(`{"type":"object",
				"properties":{"id":{"type":"string","x-mcp-filter":"false"},"x-mcp-filter":{"type":"string"},"c":{"const":{"x-mcp-filter":1}}},
				"$defs":{"d":{"items":{"x-mcp-filter":0}}}}`)},
			want: []string{
				`topic "a": payload_schema.$defs.d.items.x-mcp-filter must be a boolean`,
				`topic "a": payload_schema.properties.id.x-mcp-filter must be a boolean`,
			},
		},

		// Deprecation.
		{
			name:   "replaced_by",
			topics: []string{"a", "b", "c", "d", "e", "new"},
			defs: Definitions{
				"a":   {ReplacedBy: "new"},
				"b":   {Deprecated: true, ReplacedBy: "missing"},
				"c":   {Deprecated: true, ReplacedBy: "c"},
				"d":   with(schemaDef(object), func(d *Definition) { d.MCP, d.Deprecated, d.ReplacedBy = mcp, true, "new" }),
				"e":   with(schemaDef(object), func(d *Definition) { d.MCP, d.Deprecated, d.ReplacedBy = mcp, true, "f" }),
				"new": schemaDef(object),
			},
			want: []string{
				`topic "a": replaced_by requires deprecated: true`,
				`topic "b": replaced_by "missing" is not in TOPICS`,
				`topic "c": replaced_by can't name the topic itself`,
				`topic "d": replaced_by "new" must be MCP-enabled because this topic is`,
				`topic "e": replaced_by "f" is not in TOPICS`,
			},
		},

		// Every problem is reported, sorted.
		{
			name:   "aggregated",
			topics: []string{"a", "b"},
			defs: Definitions{
				"b": {Name: "x", MCP: mcp},
				"a": schemaDef(`{"type":1}`),
				"z": {},
			},
			want: []string{
				`topic "a": payload_schema.type: must be array`,
				`topic "a": payload_schema.type: must be one of the allowed values`,
				`topic "b": mcp.enabled requires payload_schema`,
				`topic "b": name "x" must match the topic key`,
				`topic "z" is not in TOPICS`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCatalog(tc.topics, tc.defs)
			if tc.want == nil {
				require.NoError(t, err)
				require.NotNil(t, c)
				return
			}
			assert.Nil(t, c)
			assert.Equal(t, tc.want, configProblems(t, err))
		})
	}
}

func TestConfigErrorMessage(t *testing.T) {
	_, err := NewCatalog([]string{"a"}, Definitions{"b": {}})
	assert.EqualError(t, err, `invalid topic schemas: topic "b" is not in TOPICS`)
	_, err = NewCatalog([]string{"a"}, Definitions{"b": {}, "c": {}})
	assert.EqualError(t, err, "invalid topic schemas:\n  - topic \"b\" is not in TOPICS\n  - topic \"c\" is not in TOPICS")
}

func TestUnsupportedPatternOutsideMCPIsAWarning(t *testing.T) {
	schema := `{"type":"object","properties":{"code":{"type":"string","pattern":"^(?!x)"}}}`
	c, err := NewCatalog([]string{"a"}, Definitions{"a": schemaDef(schema)})
	require.NoError(t, err)
	assert.Equal(t, []string{
		`topic "a": payload_schema pattern "^(?!x)" is not supported by Go's RE2 syntax: ` +
			"error parsing regexp: invalid or unsupported Perl syntax: `(?!`" +
			"; the topic isn't MCP-enabled, so the schema is kept",
	}, c.Warnings())
	topic, _ := c.Topic("a")
	assert.JSONEq(t, schema, string(topic.PayloadSchema), "the schema is kept")
	assert.Contains(t, c.Snapshot().Topics, "a")
}

func TestCompilerRefusesExternalResources(t *testing.T) {
	// NewCatalog rejects non-local references before compiling; the loader
	// refuses them anyway.
	for _, ref := range []string{
		"file:///etc/passwd",
		"http://example.com/schema.json",
		"https://example.com/schema.json",
		"other.json",
	} {
		t.Run(ref, func(t *testing.T) {
			_, _, err := compileSchema(map[string]any{"$ref": ref})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "external references are not allowed")
		})
	}
	// Relative references resolve against the in-memory location, not a
	// file system path or the document itself.
	_, _, err := compileSchema(map[string]any{"$ref": "other.json"})
	assert.Contains(t, sanitizeCompileError(err), `failing loading "https://outpost.invalid/other.json"`)
}

func TestNewCatalogBoundsProblems(t *testing.T) {
	names := func(prefix string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%s%03d", prefix, i)
		}
		return out
	}
	defsOf := func(names []string) Definitions {
		defs := Definitions{}
		for _, n := range names {
			defs[n] = Definition{}
		}
		return defs
	}

	_, err := NewCatalog([]string{"a"}, defsOf(names("p", 150)))
	problems := configProblems(t, err)
	require.Len(t, problems, 101)
	assert.Equal(t, `topic "p000" is not in TOPICS`, problems[0])
	assert.Equal(t, "... and 50 more", problems[100])

	long := strings.Repeat("é", maxQuotedBytes)
	_, err = NewCatalog([]string{"a"}, Definitions{long: {}})
	assert.Equal(t, []string{`topic "` + strings.Repeat("é", maxQuotedBytes/2) + `..." is not in TOPICS`}, configProblems(t, err))
}

func TestCatalogAccessors(t *testing.T) {
	object := json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","x-mcp-filter":false}}}`)
	c, err := NewCatalog([]string{"c", "a", "b", "d", "a"}, Definitions{
		"a": {Description: "A", PayloadSchema: object, MCP: MCPSettings{Enabled: true}},
		"b": {Description: "B"},
		"c": {PayloadSchema: object, MCP: MCPSettings{Enabled: true}, Deprecated: true},
	})
	require.NoError(t, err)

	topics := c.Topics()
	require.Len(t, topics, 4, "TOPICS order, duplicates dropped")
	assert.Equal(t, []string{"c", "a", "b", "d"}, []string{topics[0].Name, topics[1].Name, topics[2].Name, topics[3].Name})
	assert.Equal(t, Topic{Name: "a", Description: "A", PayloadSchema: object, MCP: MCPSettings{Enabled: true}}, topics[1])
	d, err := json.Marshal(topics[3])
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"d","mcp":{"enabled":false}}`, string(d))
	topics[0].Name = "changed"
	assert.Equal(t, "c", c.Topics()[0].Name, "Topics returns a copy")

	_, ok := c.Topic("missing")
	assert.False(t, ok)
	assert.True(t, c.HasSchemas())
	assert.True(t, c.MCPEnabled())
	assert.Equal(t, []string{"c", "a"}, c.MCPTopics())
	events := c.MCPEvents()
	require.Len(t, events, 2)
	assert.Equal(t, "c", events[0].Name)
	assert.Equal(t, "a", events[1].Name)
	ev, ok := c.MCPEvent("a")
	require.True(t, ok)
	assert.Same(t, events[1], ev)
	_, ok = c.MCPEvent("b")
	assert.False(t, ok)
	assert.Nil(t, c.Arguments("b"))
	assert.Empty(t, c.Warnings())

	assert.Equal(t, Snapshot{Topics: map[string]SnapshotTopic{
		"a": {MCPEnabled: true, PayloadSchema: object},
		"b": {},
		"c": {MCPEnabled: true, PayloadSchema: object},
	}}, c.Snapshot(), "payload schemas as configured, x-mcp-filter included")
}

func TestEmptyAndNilCatalogs(t *testing.T) {
	for name, c := range map[string]*Catalog{"empty": EmptyCatalog([]string{"a"}), "nil": nil} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, c.HasSchemas())
			assert.False(t, c.MCPEnabled())
			assert.Empty(t, c.MCPTopics())
			assert.Empty(t, c.MCPEvents())
			assert.Empty(t, c.Warnings())
			assert.Empty(t, c.Arguments("a"))
			assert.Equal(t, Snapshot{Topics: map[string]SnapshotTopic{}}, c.Snapshot())
			assert.Equal(t, []string{"arguments: topic is not MCP-enabled"}, c.ValidateArguments("a", []byte(`{}`)))
			_, ok := c.MCPEvent("a")
			assert.False(t, ok)
		})
	}
	assert.Equal(t, []Topic{{Name: "a"}}, EmptyCatalog([]string{"a"}).Topics())
	assert.Equal(t, []Topic{}, (*Catalog)(nil).Topics())
	assert.Equal(t, []Topic{}, EmptyCatalog(nil).Topics(), "never nil, so it encodes as []")

	c, err := NewCatalog([]string{"a"}, nil)
	require.NoError(t, err)
	assert.Equal(t, EmptyCatalog([]string{"a"}), c)
	c, err = NewCatalog(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, c.Topics())
}

// sentinel is planted in every failing value and key. It must never show up
// in validation errors, which are returned to MCP clients and logged.
const sentinel = "s3cr3t-PII-c4n4ry"

func TestArgumentErrorsNeverContainValues(t *testing.T) {
	s := sentinel
	c := orderCreatedCatalog(t)
	for _, args := range []string{
		`{"` + s + `": 1}`,
		`{"currency": "` + strings.Repeat(s, 30) + `"}`,
		`{"currency": ["` + s + `", 1]}`,
		`{"total": {"$gte": "` + s + `", "` + s + `": 1}}`,
		`{"total": "` + s + `"}`,
		`{"total": ` + s + `}`,
	} {
		errs := c.ValidateArguments("order.created", []byte(args))
		require.NotEmpty(t, errs, args)
		for _, line := range errs {
			assert.NotContains(t, line, sentinel, args)
		}
	}
}

func TestValidateArguments(t *testing.T) {
	c := orderCreatedCatalog(t)
	const topic = "order.created"
	values := func(n int) string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf(`"C%d"`, i)
		}
		return "[" + strings.Join(out, ",") + "]"
	}

	for _, args := range []string{
		`{"total":{"$gte":100},"currency":"USD"}`,
		`{"currency":["USD","EUR"]}`,
		`{"total":[1,2.5]}`,
		`{"total":{"$gt":1,"$lte":10}}`,
		`{"total":42}`,
		`{"currency":` + values(100) + `}`,
		`{"currency":"` + strings.Repeat("x", 256) + `"}`,
		`{"currency":"` + strings.Repeat("é", 256) + `"}`,
		`{"currency":["` + strings.Repeat("x", 256) + `"]}`,
		`{"total":1}` + strings.Repeat(" ", 16<<10-11),
		`{}`,
		``,
	} {
		assert.Nil(t, c.ValidateArguments(topic, []byte(args)), args)
	}

	for _, tc := range []struct {
		args string
		want []string
	}{
		{`{"orderId":"o_1"}`, []string{"arguments: has properties that are not allowed"}},
		{`{"createdAt":"2026-10-09T00:00:00Z"}`, []string{"arguments: has properties that are not allowed"}},
		{`{"total":"100"}`, []string{
			"arguments.total: must be array",
			"arguments.total: must be number",
			"arguments.total: must be object",
		}},
		{`{"currency":5}`, []string{"arguments.currency: must be array", "arguments.currency: must be string"}},
		{`{"total":{}}`, []string{
			"arguments.total: must be array",
			"arguments.total: must be number",
			"arguments.total: must have at least 1 property",
		}},
		{`{"total":{"$in":[1]}}`, []string{
			"arguments.total: has properties that are not allowed",
			"arguments.total: must be array",
			"arguments.total: must be number",
		}},
		{`{"total":{"$gte":"1"}}`, []string{
			"arguments.total.$gte: must be number",
			"arguments.total: must be array",
			"arguments.total: must be number",
		}},
		{`{"currency":{"$gte":"USD"}}`, []string{"arguments.currency: must be array", "arguments.currency: must be string"}},
		{`{"currency":[]}`, []string{"arguments.currency: must be string", "arguments.currency: must have at least 1 item"}},
		// Lists and strings past the inferred limits fail before the schema
		// is checked, with only the size problem reported.
		{`{"currency":` + values(101) + `}`, []string{"arguments.currency: must have at most 100 items"}},
		{`{"currency":"` + strings.Repeat("x", 257) + `"}`, []string{"arguments.currency: must be at most 256 characters"}},
		{`{"currency":"` + strings.Repeat("é", 257) + `"}`, []string{"arguments.currency: must be at most 256 characters"}},
		{`{"currency":["` + strings.Repeat("x", 257) + `"]}`, []string{"arguments.currency[0]: must be at most 256 characters"}},
		{`{"total":{"$gte":` + values(101) + `}}`, []string{"arguments.total.$gte: must have at most 100 items"}},
		{`{"currency":"USD","x":` + values(101) + `,"total":"` + strings.Repeat("x", 257) + `"}`, []string{"arguments.total: must be at most 256 characters"}},
		{`{"total":1e5000}`, []string{"arguments.total: must be a number of at most 1000 characters with an exponent between -1000 and 1000"}},
		{`null`, []string{"arguments: must be object"}},
		{`{"total":`, []string{"arguments: must be valid JSON"}},
		{`{"total":1}` + strings.Repeat(" ", maxArgumentsBytes), []string{"arguments: exceed the size limit"}},
		{`{"total":1}` + strings.Repeat(" ", 16<<10-10), []string{"arguments: exceed the size limit"}},
	} {
		assert.Equal(t, tc.want, c.ValidateArguments(topic, []byte(tc.args)), tc.args)
	}

	assert.Equal(t, []string{"arguments: topic is not MCP-enabled"}, c.ValidateArguments("unknown", []byte(`{}`)))
	off, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(`{"type":"object"}`)}})
	require.NoError(t, err)
	assert.Equal(t, []string{"arguments: topic is not MCP-enabled"}, off.ValidateArguments("t", []byte(`{}`)))
}

func TestValidateArgumentsBoundsWork(t *testing.T) {
	// The validator checks every list item against the enum even past
	// maxItems, comparing numbers as exact rationals, so long lists must
	// fail before the schema is checked.
	enum := make([]string, 1000)
	for i := range enum {
		enum[i] = fmt.Sprint(i + 1)
	}
	schema := `{"type":"object","properties":{"status":{"type":"integer","enum":[` + strings.Join(enum, ",") + `]}}}`
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(schema), MCP: MCPSettings{Enabled: true}}})
	require.NoError(t, err)

	items := strings.TrimSuffix(strings.Repeat("1,", (maxArgumentsBytes-20)/2), ",")
	args := []byte(`{"status":[` + items + `]}`)
	require.LessOrEqual(t, len(args), maxArgumentsBytes)
	start := time.Now()
	assert.Equal(t, []string{"arguments.status: must have at most 100 items"}, c.ValidateArguments("t", args))
	assert.Less(t, time.Since(start), time.Second)
	assert.Nil(t, c.ValidateArguments("t", []byte(`{"status":[`+strings.TrimSuffix(strings.Repeat("1000,", 100), ",")+`]}`)))
}

func TestValidateArgumentsEnums(t *testing.T) {
	schema := `{"type":"object","properties":{
		"status":{"type":"integer","enum":[1,2,30,null]},
		"ratio":{"enum":[0.5,1e2]},
		"code":{"type":"string","enum":["A","B"]},
		"flag":{"const":true},
		"open":{"type":"string"}}}`
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(schema), MCP: MCPSettings{Enabled: true}}})
	require.NoError(t, err)

	ev, ok := c.MCPEvent("t")
	require.True(t, ok)
	assert.Contains(t, string(ev.InputSchema), `"enum":[1,2,30]`, "the inputSchema still lists the enums")
	assert.Contains(t, string(ev.InputSchema), `"enum":["A","B"]`)

	for _, args := range []string{
		`{"status":2}`,
		`{"status":[1,30]}`,
		// Numbers match by value.
		`{"status":[2.0,20e-1,0.2E1,3e1,300e-1]}`,
		`{"ratio":[0.50,5e-1,100,1e+2,0.1e3]}`,
		`{"status":{"$gte":4,"$lt":1000}}`,
		`{"code":["A","B"],"flag":true,"open":"anything"}`,
	} {
		assert.Nil(t, c.ValidateArguments("t", []byte(args)), args)
	}

	for _, tc := range []struct {
		args string
		want []string
	}{
		{`{"status":4}`, []string{"arguments.status: must be one of the allowed values"}},
		{`{"status":[1,4,30,0]}`, []string{
			"arguments.status[1]: must be one of the allowed values",
			"arguments.status[3]: must be one of the allowed values",
		}},
		{`{"ratio":0.05,"code":["A","a"],"flag":false}`, []string{
			"arguments.code[1]: must be one of the allowed values",
			"arguments.flag: must be one of the allowed values",
			"arguments.ratio: must be one of the allowed values",
		}},
		// The schema is checked first.
		{`{"code":5,"status":4}`, []string{"arguments.code: must be array", "arguments.code: must be string"}},
		{`{"status":null}`, []string{
			"arguments.status: must be array",
			"arguments.status: must be integer",
			"arguments.status: must be object",
		}},
	} {
		assert.Equal(t, tc.want, c.ValidateArguments("t", []byte(tc.args)), tc.args)
	}

	enum := make([]string, 30)
	for i := range enum {
		enum[i] = strconv.Itoa(i)
	}
	many, err := NewCatalog([]string{"t"}, Definitions{"t": {
		PayloadSchema: json.RawMessage(`{"type":"object","properties":{"a":{"enum":[` + strings.Join(enum, ",") + `]},"b":{"enum":[0]}}}`),
		MCP:           MCPSettings{Enabled: true},
	}})
	require.NoError(t, err)
	errs := many.ValidateArguments("t", []byte(`{"a":[`+strings.Join(enum, ",")+`],"b":[`+strings.Join(enum, ",")+`]}`))
	require.Len(t, errs, 21)
	assert.Equal(t, "arguments.b[10]: must be one of the allowed values", errs[0])
	assert.Equal(t, "... and 9 more", errs[20])
}

func TestValidateArgumentsLargeEnum(t *testing.T) {
	// The validator compares a value with every enum value, numbers as
	// exact rationals, so enums are checked against a set instead.
	enum := make([]string, 12_000)
	for i := range enum {
		enum[i] = strconv.Itoa(1000 + i)
	}
	schema := `{"type":"object","properties":{"p":{"type":"integer","enum":[` + strings.Join(enum, ",") + `]}}}`
	require.Less(t, len(schema), maxMCPPayloadSchemaBytes)
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(schema), MCP: MCPSettings{Enabled: true}}})
	require.NoError(t, err)

	list := func(v string) []byte {
		return []byte(`{"p":[` + strings.TrimSuffix(strings.Repeat(v+",", maxArgumentListItems), ",") + `]}`)
	}
	start := time.Now()
	for range 10 {
		assert.Nil(t, c.ValidateArguments("t", list("12999")))
		assert.Len(t, c.ValidateArguments("t", list("1")), 21)
	}
	assert.Less(t, time.Since(start), 200*time.Millisecond, "20 calls")
}

func TestCatalogConcurrentUse(t *testing.T) {
	// Run with -race: the catalog is shared by every MCP request without
	// locks.
	schema := `{"type":"object","properties":{
		"total":{"type":"number","minimum":0},
		"currency":{"type":"string","enum":["USD","EUR"]},
		"day":{"type":"string","format":"date"},
		"code":{"type":"string","pattern":"^[A-Z]{3}$"},
		"items":{"type":"array","items":{"$ref":"#/$defs/item"}}},
		"$defs":{"item":{"type":"object","properties":{"qty":{"type":"integer","multipleOf":2}},"required":["qty"]}}}`
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {
		PayloadSchema: json.RawMessage(schema),
		MCP:           MCPSettings{Enabled: true},
	}})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				assert.Nil(t, c.ValidateArguments("t", []byte(`{"total":{"$gte":1},"currency":["USD"],"day":{"$lt":"2027-01-01"}}`)))
				assert.Len(t, c.ValidateArguments("t", []byte(`{"total":"x","code":1}`)), 5)
				assert.Len(t, c.MCPEvents(), 1)
				assert.Len(t, c.Arguments("t"), 4)
				assert.Len(t, c.Snapshot().Topics, 1)
				assert.Len(t, c.Topics(), 1)
			}
		}()
	}
	wg.Wait()
}

package topicschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

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
		{
			name:     "empty TOPICS with imported definitions",
			imported: Definitions{"a": {}},
			want:     []string{"topic schemas require TOPICS to list the topics"},
		},

		// Topic fields.
		{
			name:   "name differs from key",
			topics: []string{"a"},
			defs:   Definitions{"a": {Name: "b"}},
			want:   []string{`topic "a": name "b" must match the topic key`},
		},
		{
			name:   "unknown validation mode",
			topics: []string{"a"},
			defs:   Definitions{"a": with(schemaDef(object), func(d *Definition) { d.Validation = "strict" })},
			want:   []string{`topic "a": validation "strict" must be "off", "warn" or "enforce"`},
		},
		{
			name:   "validation without payload_schema",
			topics: []string{"a", "b", "c"},
			defs: Definitions{
				"a": {Validation: ValidationWarn},
				"b": {Validation: ValidationEnforce, PayloadSchema: json.RawMessage(`null`)},
				"c": {Validation: ValidationOff},
			},
			want: []string{
				`topic "a": validation "warn" requires payload_schema`,
				`topic "b": validation "enforce" requires payload_schema`,
			},
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
			topics: []string{"a", "b"},
			defs: Definitions{
				"a": schemaDef(`{"properties":{"x":{"$ref":"#/$defs/missing"}}}`),
				"b": schemaDef(`{"$ref":"#nowhere"}`),
			},
			want: []string{
				`topic "a": payload_schema: json-pointer in "#/$defs/missing" not found`,
				`topic "b": payload_schema: anchor in "#nowhere" not found in schema ""`,
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
				`topic "c": payload_schema.$defs.x.$schema must be "https://json-schema.org/draft/2020-12/schema"`,
			},
		},
		{
			name:   "patterns RE2 can't compile in topics that use the schema",
			topics: []string{"warn", "enforce", "mcp", "props"},
			defs: Definitions{
				"warn":    with(schemaDef(`{"pattern":"(?=x)"}`), func(d *Definition) { d.Validation = ValidationWarn }),
				"enforce": with(schemaDef(`{"properties":{"a":{"pattern":"(?=x)"}}}`), func(d *Definition) { d.Validation = ValidationEnforce }),
				"mcp":     with(schemaDef(`{"type":"object","properties":{"a":{"pattern":"(?=x)"}}}`), func(d *Definition) { d.MCP = mcp }),
				"props":   with(schemaDef(`{"patternProperties":{"(?=x)":true}}`), func(d *Definition) { d.Validation = ValidationWarn }),
			},
			want: []string{
				`topic "enforce": payload_schema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2,
				`topic "mcp": payload_schema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2,
				`topic "props": payload_schema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2,
				`topic "warn": payload_schema pattern "(?=x)" is not supported by Go's RE2 syntax: ` + re2,
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
				"b": {Name: "x", Validation: ValidationWarn, MCP: mcp},
				"a": schemaDef(`{"type":1}`),
				"z": {},
			},
			want: []string{
				`topic "a": payload_schema.type: must be array`,
				`topic "a": payload_schema.type: must be one of the allowed values`,
				`topic "b": mcp.enabled requires payload_schema`,
				`topic "b": name "x" must match the topic key`,
				`topic "b": validation "warn" requires payload_schema`,
				`topic "z" is not in TOPICS`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCatalog(tc.topics, tc.defs, WithImported(tc.imported))
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

func TestUnsupportedPatternInOffTopicIsAWarning(t *testing.T) {
	schema := `{"type":"object","properties":{"code":{"type":"string","pattern":"^(?!x)"}}}`
	c, err := NewCatalog([]string{"a"}, Definitions{"a": schemaDef(schema)})
	require.NoError(t, err)
	assert.Equal(t, []string{
		`topic "a": payload_schema pattern "^(?!x)" is not supported by Go's RE2 syntax: ` +
			"error parsing regexp: invalid or unsupported Perl syntax: `(?!`" +
			"; validation is off, so the schema is kept but can't be used to validate",
	}, c.Warnings())
	topic, _ := c.Topic("a")
	assert.JSONEq(t, schema, string(topic.PayloadSchema), "the schema is kept")
	assert.Equal(t, ValidationResult{Mode: ValidationOff}, c.ValidateData("a", []byte(`{"code":"x"}`)))
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

func TestNewCatalogImported(t *testing.T) {
	object := json.RawMessage(`{"type":"object"}`)
	c, err := NewCatalog([]string{"a", "b", " c"},
		Definitions{"b": {Description: "explicit"}},
		WithImported(Definitions{
			"a": {Description: "imported", PayloadSchema: object, MCP: MCPSettings{Enabled: true}},
			"b": {Description: "imported", PayloadSchema: object, MCP: MCPSettings{Enabled: true}},
			"c": {Description: "imported"},
			"z": {Description: "imported"},
		}))
	require.NoError(t, err)
	a, _ := c.Topic("a")
	assert.Equal(t, "imported", a.Description)
	assert.True(t, a.MCP.Enabled)
	b, _ := c.Topic("b")
	assert.Equal(t, Topic{Name: "b", Description: "explicit", Validation: ValidationOff}, b, "explicit definitions replace the whole imported entry")
	assert.Equal(t, []string{"a"}, c.MCPTopics())
	assert.Equal(t, []string{
		`imported topic "c" is not in TOPICS and was skipped (did you mean " c"?)`,
		`imported topic "z" is not in TOPICS and was skipped`,
	}, c.Warnings())
}

func TestCatalogAccessors(t *testing.T) {
	object := json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","x-mcp-filter":false}}}`)
	c, err := NewCatalog([]string{"c", "a", "b", "d", "a"}, Definitions{
		"a": {Description: "A", PayloadSchema: object, MCP: MCPSettings{Enabled: true}, Validation: ValidationWarn},
		"b": {Description: "B"},
		"c": {PayloadSchema: object, MCP: MCPSettings{Enabled: true}, Deprecated: true},
	})
	require.NoError(t, err)

	topics := c.Topics()
	require.Len(t, topics, 4, "TOPICS order, duplicates dropped")
	assert.Equal(t, []string{"c", "a", "b", "d"}, []string{topics[0].Name, topics[1].Name, topics[2].Name, topics[3].Name})
	assert.Equal(t, Topic{Name: "a", Description: "A", PayloadSchema: object, Validation: ValidationWarn, MCP: MCPSettings{Enabled: true}}, topics[1])
	d, err := json.Marshal(topics[3])
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"d","validation":"off","mcp":{"enabled":false}}`, string(d))
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
			assert.Equal(t, ValidationResult{Mode: ValidationOff}, c.ValidateData("a", []byte(`{}`)))
			assert.Equal(t, []string{"arguments: topic is not MCP-enabled"}, c.ValidateArguments("a", []byte(`{}`)))
			_, ok := c.MCPEvent("a")
			assert.False(t, ok)
		})
	}
	assert.Equal(t, []Topic{{Name: "a", Validation: ValidationOff}}, EmptyCatalog([]string{"a"}).Topics())
	assert.Equal(t, []Topic{}, (*Catalog)(nil).Topics())

	c, err := NewCatalog([]string{"a"}, nil)
	require.NoError(t, err)
	assert.Equal(t, EmptyCatalog([]string{"a"}), c)
	c, err = NewCatalog(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, c.Topics())
}

const orderSchema = `{
	"type": "object",
	"properties": {
		"id": {"type": "string", "x-mcp-filter": false},
		"total": {"type": "number", "minimum": 0},
		"currency": {"type": "string", "enum": ["USD", "EUR"]},
		"day": {"type": "string", "format": "date"},
		"pad": {"type": "string"}
	},
	"required": ["id", "total"]
}`

func validationCatalog(t *testing.T, opts ...Option) *Catalog {
	t.Helper()
	def := func(mode ValidationMode) Definition {
		return Definition{PayloadSchema: json.RawMessage(orderSchema), Validation: mode}
	}
	c, err := NewCatalog([]string{"enforced", "warned", "off", "plain", "*"}, Definitions{
		"enforced": def(ValidationEnforce),
		"warned":   def(ValidationWarn),
		"off":      def(ValidationOff),
	}, opts...)
	require.NoError(t, err)
	return c
}

func TestValidateData(t *testing.T) {
	c := validationCatalog(t)
	valid := []byte(`{"id":"o_1","total":12.5,"currency":"USD","day":"2026-10-09"}`)
	invalid := []byte(`{"total":-1,"currency":"GBP","day":"2026-02-30"}`)
	invalidErrors := []string{
		"data.currency: must be one of the allowed values",
		"data.day: must be a valid date",
		"data.total: must be >= 0",
		`data: missing required property "id"`,
	}

	for _, topic := range []string{"", "*", "unknown", "plain", "off"} {
		assert.Equal(t, ValidationResult{Mode: ValidationOff}, c.ValidateData(topic, invalid), topic)
	}
	assert.Equal(t, ValidationResult{Mode: ValidationEnforce, Checked: true, Valid: true}, c.ValidateData("enforced", valid))
	assert.Equal(t, ValidationResult{Mode: ValidationWarn, Checked: true, Valid: true}, c.ValidateData("warned", valid))
	assert.Equal(t, ValidationResult{Mode: ValidationEnforce, Checked: true, Errors: invalidErrors}, c.ValidateData("enforced", invalid))
	assert.Equal(t, ValidationResult{Mode: ValidationWarn, Checked: true, Errors: invalidErrors}, c.ValidateData("warned", invalid))

	assert.Equal(t, []string{"data: must be valid JSON"}, c.ValidateData("enforced", []byte(`{"id":`)).Errors)
	assert.Equal(t, []string{"data: must be valid JSON"}, c.ValidateData("enforced", []byte(`{} {}`)).Errors)
	assert.Equal(t, []string{"data: must be object"}, c.ValidateData("enforced", []byte(`[]`)).Errors)
}

func TestValidateDataSizeLimit(t *testing.T) {
	padded := func(size int) []byte {
		prefix := `{"id":"o_1","total":1,"pad":"`
		return []byte(prefix + strings.Repeat("x", size-len(prefix)-2) + `"}`)
	}

	c := validationCatalog(t)
	atLimit, overLimit := padded(1<<20), padded(1<<20+1)
	assert.Equal(t, ValidationResult{Mode: ValidationEnforce, Checked: true, Valid: true}, c.ValidateData("enforced", atLimit), "1 MiB by default")
	assert.Equal(t, ValidationResult{
		Mode:    ValidationEnforce,
		Checked: true,
		Errors:  []string{"data exceeds the schema validation size limit"},
	}, c.ValidateData("enforced", overLimit))
	assert.Equal(t, ValidationResult{Mode: ValidationWarn, SkippedTooLarge: true}, c.ValidateData("warned", overLimit))
	assert.Equal(t, ValidationResult{Mode: ValidationOff}, c.ValidateData("off", overLimit))

	c = validationCatalog(t, WithMaxValidationBytes(100))
	assert.True(t, c.ValidateData("enforced", padded(100)).Valid)
	assert.Equal(t, []string{"data exceeds the schema validation size limit"}, c.ValidateData("enforced", padded(101)).Errors)
	assert.Equal(t, ValidationResult{Mode: ValidationWarn, SkippedTooLarge: true}, c.ValidateData("warned", padded(101)))
}

func TestValidateDataLargeNumbers(t *testing.T) {
	// Exact rational arithmetic on numbers like 1e999999 takes milliseconds
	// each, so they fail validation instead of being checked.
	c := validationCatalog(t)
	want := []string{"data.total: must be a number of at most 1000 characters with an exponent between -1000 and 1000"}
	for _, n := range []string{"1e1001", "1E-1001", "1e+99999999999999999999", "1" + strings.Repeat("0", 1000)} {
		assert.Equal(t, want, c.ValidateData("enforced", []byte(`{"id":"x","total":`+n+`}`)).Errors, n)
	}
	for _, n := range []string{"1e1000", "1.5e-1000", "1" + strings.Repeat("0", 999)} {
		assert.True(t, c.ValidateData("enforced", []byte(`{"id":"x","total":`+n+`}`)).Valid, n)
	}
	// Anywhere in the value, schema or not.
	assert.Equal(t, []string{"data.*[1]: must be a number of at most 1000 characters with an exponent between -1000 and 1000"},
		c.ValidateData("enforced", []byte(`{"id":"x","total":1,"extra":[1,1e5000]}`)).Errors)
}

func TestFormats(t *testing.T) {
	// Only date and date-time are asserted; every other format is an
	// annotation, as JSON Schema 2020-12 specifies.
	formats := []string{
		"email", "idn-email", "uri", "uri-reference", "iri", "iri-reference", "uri-template",
		"hostname", "idn-hostname", "ipv4", "ipv6", "uuid", "regex", "time", "duration", "period",
		"json-pointer", "relative-json-pointer", "semver", "unknown-format",
	}
	props := []string{`"day":{"type":"string","format":"date"}`, `"at":{"type":"string","format":"date-time"}`}
	data := []string{`"day":"2026-10-09"`, `"at":"2026-10-09T10:00:00.5+02:00"`}
	for _, f := range formats {
		props = append(props, fmt.Sprintf(`%q:{"type":"string","format":%q}`, f, f))
		data = append(data, fmt.Sprintf(`%q:"not-a-%s ((["`, f, f))
	}
	schema := `{"type":"object","properties":{` + strings.Join(props, ",") + `}}`
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(schema), Validation: ValidationEnforce}})
	require.NoError(t, err)

	result := c.ValidateData("t", []byte(`{`+strings.Join(data, ",")+`}`))
	assert.True(t, result.Valid, "%v", result.Errors)
	assert.True(t, c.ValidateData("t", []byte(`{"email":"not-an-email"}`)).Valid)
	assert.Equal(t, []string{"data.at: must be a valid date-time"}, c.ValidateData("t", []byte(`{"at":"nope"}`)).Errors)
	assert.Equal(t, []string{"data.day: must be a valid date"}, c.ValidateData("t", []byte(`{"day":"2026-02-30"}`)).Errors)
	assert.Equal(t, []string{"data.day: must be a valid date"}, c.ValidateData("t", []byte(`{"day":"2026-10-09T10:00:00Z"}`)).Errors)

	untyped, err := NewCatalog([]string{"t"}, Definitions{"t": {
		PayloadSchema: json.RawMessage(`{"properties":{"day":{"format":"date"},"at":{"format":"date-time"}}}`),
		Validation:    ValidationEnforce,
	}})
	require.NoError(t, err)
	assert.True(t, untyped.ValidateData("t", []byte(`{"day":5,"at":true}`)).Valid, "formats only apply to strings")
}

// sentinel is planted in every failing value and key. It must never show up
// in validation errors, which are returned in 422 bodies and logged.
const sentinel = "s3cr3t-PII-c4n4ry"

func TestValidationErrorsNeverContainValues(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "pattern": "^[0-9]+$"},
			"format": {"type": "string", "format": "date-time"},
			"day": {"type": "string", "format": "date"},
			"enum": {"enum": ["a", "b"]},
			"const": {"const": "a"},
			"minLength": {"type": "string", "minLength": 100},
			"maxLength": {"type": "string", "maxLength": 3},
			"type": {"type": "integer"},
			"number": {"type": "number", "minimum": 1e10, "multipleOf": 7},
			"strict": {"type": "object", "properties": {"a": {}}, "additionalProperties": false},
			"loose": {"type": "object", "additionalProperties": {"type": "integer"}},
			"patterned": {"type": "object", "patternProperties": {"^s": {"type": "integer"}}},
			"names": {"type": "object", "propertyNames": {"maxLength": 3}},
			"unevaluated": {"type": "object", "unevaluatedProperties": {"type": "integer"}},
			"list": {"type": "array", "items": {"type": "integer"}, "uniqueItems": true},
			"nested": {"type": "array", "items": {"$ref": "#/$defs/item"}},
			"choice": {"oneOf": [{"type": "integer"}, {"type": "boolean"}]},
			"negated": {"not": {"type": "string"}}
		},
		"$defs": {"item": {"type": "object", "properties": {"sku": {"type": "string", "pattern": "^[A-Z]{3}$"}}, "additionalProperties": false}}
	}`
	s := sentinel
	data := `{
		"pattern": "` + s + `",
		"format": "` + s + `",
		"day": "` + s + `",
		"enum": "` + s + `",
		"const": "` + s + `",
		"minLength": "` + s + `",
		"maxLength": "` + s + `",
		"type": "` + s + `",
		"number": 987654321,
		"strict": {"` + s + `": "` + s + `"},
		"loose": {"` + s + `": "` + s + `"},
		"patterned": {"s` + s + `": "` + s + `"},
		"names": {"` + s + `": 1},
		"unevaluated": {"` + s + `": "` + s + `"},
		"list": ["` + s + `", "` + s + `"],
		"nested": [{"sku": "` + s + `", "` + s + `": 1}],
		"choice": "` + s + `",
		"negated": "` + s + `",
		"` + s + `": "` + s + `"
	}`
	for _, mode := range []ValidationMode{ValidationWarn, ValidationEnforce} {
		c, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(schema), Validation: mode}})
		require.NoError(t, err)
		result := c.ValidateData("t", []byte(data))
		require.True(t, result.Checked)
		require.False(t, result.Valid)
		require.Len(t, result.Errors, maxReportedErrors+1, "every planted failure is reported")
		for _, line := range result.Errors {
			assert.NotContains(t, line, sentinel)
			assert.NotContains(t, line, "987654321")
		}
		for _, bad := range []string{`{"pattern": ` + s + `}`, `{"pattern": "` + s + `"` + s + `}`} {
			for _, line := range c.ValidateData("t", []byte(bad)).Errors {
				assert.NotContains(t, line, sentinel, "parse errors quote input")
			}
		}
	}

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
		{`{"currency":` + values(101) + `}`, []string{"arguments.currency: must be string", "arguments.currency: must have at most 100 items"}},
		{`{"currency":"` + strings.Repeat("x", 257) + `"}`, []string{
			"arguments.currency: must be array",
			"arguments.currency: must be at most 256 characters",
		}},
		{`{"currency":["` + strings.Repeat("x", 257) + `"]}`, []string{
			"arguments.currency: must be string",
			"arguments.currency[0]: must be at most 256 characters",
		}},
		{`{"total":1e5000}`, []string{"arguments.total: must be a number of at most 1000 characters with an exponent between -1000 and 1000"}},
		{`null`, []string{"arguments: must be object"}},
		{`{"total":`, []string{"arguments: must be valid JSON"}},
		{`{"total":1}` + strings.Repeat(" ", maxArgumentsBytes), []string{"arguments: exceed the size limit"}},
	} {
		assert.Equal(t, tc.want, c.ValidateArguments(topic, []byte(tc.args)), tc.args)
	}

	assert.Equal(t, []string{"arguments: topic is not MCP-enabled"}, c.ValidateArguments("unknown", []byte(`{}`)))
	off, err := NewCatalog([]string{"t"}, Definitions{"t": {PayloadSchema: json.RawMessage(`{"type":"object"}`)}})
	require.NoError(t, err)
	assert.Equal(t, []string{"arguments: topic is not MCP-enabled"}, off.ValidateArguments("t", []byte(`{}`)))
}

func TestCatalogConcurrentUse(t *testing.T) {
	// Run with -race: the catalog is shared by every publish and MCP request
	// without locks.
	schema := `{"type":"object","properties":{
		"total":{"type":"number","minimum":0},
		"currency":{"type":"string","enum":["USD","EUR"]},
		"day":{"type":"string","format":"date"},
		"code":{"type":"string","pattern":"^[A-Z]{3}$"},
		"items":{"type":"array","items":{"$ref":"#/$defs/item"}}},
		"$defs":{"item":{"type":"object","properties":{"qty":{"type":"integer","multipleOf":2}},"required":["qty"]}}}`
	c, err := NewCatalog([]string{"t"}, Definitions{"t": {
		PayloadSchema: json.RawMessage(schema),
		Validation:    ValidationEnforce,
		MCP:           MCPSettings{Enabled: true},
	}})
	require.NoError(t, err)

	valid := []byte(`{"total":1,"currency":"USD","day":"2026-10-09","code":"ABC","items":[{"qty":2}]}`)
	invalid := []byte(`{"total":-1.5,"currency":"GBP","day":"x","code":"abc","items":[{"qty":3},{}]}`)
	wantErrors := c.ValidateData("t", invalid).Errors
	require.Len(t, wantErrors, 6)

	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				if (g+i)%2 == 0 {
					assert.True(t, c.ValidateData("t", valid).Valid)
				} else {
					assert.Equal(t, wantErrors, c.ValidateData("t", invalid).Errors)
				}
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

// benchmarkSchema resembles a typical order payload schema.
const benchmarkSchema = `{
	"type": "object",
	"properties": {
		"id": {"type": "string", "pattern": "^ord_[a-z0-9]+$"},
		"createdAt": {"type": "string", "format": "date-time"},
		"currency": {"type": "string", "enum": ["USD", "EUR", "GBP"]},
		"total": {"type": "number", "minimum": 0},
		"customer": {
			"type": "object",
			"properties": {
				"id": {"type": "string"},
				"email": {"type": "string", "format": "email"},
				"name": {"type": "string", "maxLength": 200}
			},
			"required": ["id"]
		},
		"items": {"type": "array", "items": {"$ref": "#/$defs/item"}}
	},
	"required": ["id", "createdAt", "currency", "total", "items"],
	"$defs": {
		"item": {
			"type": "object",
			"properties": {
				"sku": {"type": "string", "pattern": "^[A-Z]{3}-[0-9]{4}$"},
				"name": {"type": "string"},
				"quantity": {"type": "integer", "minimum": 1},
				"price": {"type": "number", "minimum": 0},
				"tags": {"type": "array", "items": {"type": "string"}}
			},
			"required": ["sku", "quantity", "price"],
			"additionalProperties": false
		}
	}
}`

// benchmarkPayload returns an order of about size bytes, with every item
// invalid when invalid is set.
func benchmarkPayload(size int, invalid bool) []byte {
	var b strings.Builder
	b.WriteString(`{"id":"ord_123abc","createdAt":"2026-10-09T10:00:00Z","currency":"USD","total":1234.5,` +
		`"customer":{"id":"cus_1","email":"jane@example.com","name":"Jane Doe"},"items":[`)
	for i := 0; b.Len() < size-2; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if invalid {
			fmt.Fprintf(&b, `{"sku":"bad-%d","name":"Item %d","quantity":0,"price":-1,"tags":[1]}`, i, i)
		} else {
			fmt.Fprintf(&b, `{"sku":"ABC-%04d","name":"Item %d","quantity":%d,"price":9.99,"tags":["a","b"]}`, i%10000, i, i%5+1)
		}
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func BenchmarkValidateData(b *testing.B) {
	c, err := NewCatalog([]string{"order.created"}, Definitions{"order.created": {
		PayloadSchema: json.RawMessage(benchmarkSchema),
		Validation:    ValidationEnforce,
	}})
	require.NoError(b, err)
	for _, bc := range []struct {
		name    string
		size    int
		invalid bool
	}{
		{"10KB", 10 << 10, false},
		{"500KB", 500 << 10, false},
		{"10KB_invalid", 10 << 10, true},
		{"500KB_invalid", 500 << 10, true},
	} {
		data := benchmarkPayload(bc.size, bc.invalid)
		result := c.ValidateData("order.created", data)
		require.True(b, result.Checked)
		require.Equal(b, !bc.invalid, result.Valid, "%v", result.Errors)
		b.Run(bc.name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				c.ValidateData("order.created", data)
			}
		})
	}
}

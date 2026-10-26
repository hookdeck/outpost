package topicschema

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func requireProblems(t *testing.T, err error, want ...string) {
	t.Helper()
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr), "want a *ConfigError, got %T: %v", err, err)
	require.Len(t, cfgErr.Problems, len(want), "problems: %q", cfgErr.Problems)
	for i, w := range want {
		assert.Contains(t, cfgErr.Problems[i], w)
	}
}

func TestParseDefinitions(t *testing.T) {
	const asYAML = `
# Comments are fine.
order.created:
  description: Fires when <an order> & more.
  payload_schema:
    type: object
    properties:
      zeta: {type: integer, maximum: 9007199254740993}
      alpha:
        type: number
        multipleOf: 0.10
        x-mcp-filter: false
      placedOn:
        type: string
        format: date
        default: 2026-10-09
        examples: [2026-10-09T16:58:12Z, 2026-10-09 16:58:12.5 -5]
    required: [zeta]
order.deleted: {description: null, payload_schema: null}
order.bare: {}
`
	const asJSON = `{
	"order.created": {
		"description": "Fires when <an order> & more.",
		"payload_schema": {"type": "object", "properties": {
			"zeta": {"type": "integer", "maximum": 9007199254740993},
			"alpha": {"type": "number", "multipleOf": 0.10, "x-mcp-filter": false},
			"placedOn": {"type": "string", "format": "date", "default": "2026-10-09",
				"examples": ["2026-10-09T16:58:12Z", "2026-10-09 16:58:12.5 -5"]}
		}, "required": ["zeta"]}
	},
	"order.deleted": {"description": null, "payload_schema": null},
	"order.bare": {}
}`
	fromYAML, err := ParseDefinitions([]byte(asYAML))
	require.NoError(t, err)
	fromJSON, err := ParseDefinitions([]byte(asJSON))
	require.NoError(t, err)
	assert.Equal(t, fromJSON, fromYAML)
	require.Len(t, fromYAML, 3)
	assert.Equal(t, Definition{
		Description:   "Fires when <an order> & more.",
		PayloadSchema: []byte(`{"type":"object","properties":{"zeta":{"type":"integer","maximum":9007199254740993},"alpha":{"type":"number","multipleOf":0.10,"x-mcp-filter":false},"placedOn":{"type":"string","format":"date","default":"2026-10-09","examples":["2026-10-09T16:58:12Z","2026-10-09 16:58:12.5 -5"]}},"required":["zeta"]}`),
	}, fromYAML["order.created"])
	assert.Equal(t, Definition{}, fromYAML["order.deleted"])

	t.Run("byte order mark", func(t *testing.T) {
		defs, err := ParseDefinitions([]byte("\xef\xbb\xbf{\"a\": {}}"))
		require.NoError(t, err)
		assert.Contains(t, defs, "a")
	})

	t.Run("JSON indented with tabs", func(t *testing.T) {
		defs, err := ParseDefinitions([]byte("{\n\t\"a\": {\n\t\t\"description\": \"x\"\n\t}\n}"))
		require.NoError(t, err)
		assert.Equal(t, "x", defs["a"].Description)
	})

	t.Run("JSON string escapes", func(t *testing.T) {
		defs, err := ParseDefinitions([]byte(`{"a": {"payload_schema": {"pattern": "^\u00e9\\d+$/", "description": "tab\there\u2028"}}}`))
		require.NoError(t, err)
		assert.Equal(t, `{"pattern":"^é\\d+$/","description":"tab\there\u2028"}`, string(defs["a"].PayloadSchema))
	})

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unknown field", input: "a:\n  validaton: warn\n", want: `event "a": /a/validaton (line 2): unknown field "validaton"`},
		{name: "field spelled differently", input: "a:\n  payloadSchema: {}\n", want: `unknown field "payloadSchema" (did you mean "payload_schema"?)`},
		{name: "duplicate key", input: "a:\n  payload_schema:\n    type: object\n    type: string\n", want: `event "a": /a/payload_schema (line 4): duplicate key "type"`},
		{name: "duplicate field", input: "a:\n  description: x\n  description: y\n", want: `event "a": /a/description (line 3): duplicate key "description"`},
		{name: "duplicate event", input: "a: {}\na: {}\n", want: `line 2: duplicate key "a"`},
		{name: "merge key", input: "base: &base\n  description: x\na:\n  <<: *base\n", want: `event "a": /a (line 4): merge keys (<<) are not supported`},
		{name: "null key", input: "~: {}\n", want: `line 1: mapping keys must be strings, not null`},
		{name: "complex key", input: "? [a, b]\n: {}\n", want: `line 1: mapping keys must be strings`},
		{name: "infinity", input: "a:\n  payload_schema:\n    maximum: .inf\n", want: `event "a": /a/payload_schema/maximum (line 3): ".inf" is infinite or NaN`},
		{name: "event not an object", input: "a: 5\n", want: `event "a": /a (line 1): must be an object`},
		{name: "description not a string", input: "a:\n  description: 5\n", want: `/a/description (line 2): must be a string`},
		{name: "not a mapping", input: "- a\n- b\n", want: "event definitions must be a mapping keyed by event name"},
		{name: "empty", input: "# nothing\n", want: "the definitions document is empty"},
		{name: "several documents", input: "a: {}\n---\nb: {}\n", want: "a stream with more than one document is not supported"},
		{name: "invalid", input: "a: [\n", want: "invalid YAML or JSON: line 1"},
		{name: "invalid JSON", input: `{"a": `, want: "invalid YAML or JSON"},
		{name: "alias cycle", input: "a:\n  payload_schema: &x [1, *x]\n", want: `/a/payload_schema/1 (line 2): alias *x refers to a node that contains it`},
		{name: "too deep", input: "a:\n  payload_schema: " + strings.Repeat("[", 129) + strings.Repeat("]", 129) + "\n", want: "nesting exceeds 128 levels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, err := ParseDefinitions([]byte(tc.input))
			assert.Nil(t, defs)
			requireProblems(t, err, tc.want)
		})
	}

	t.Run("problems are collected", func(t *testing.T) {
		_, err := ParseDefinitions([]byte("a: {x: 1}\nb: {y: 1}\n"))
		requireProblems(t, err, `event "a"`, `event "b"`)
	})

	t.Run("size limit", func(t *testing.T) {
		_, err := ParseDefinitions([]byte("a:\n  description: " + strings.Repeat("x", maxInputBytes)))
		requireProblems(t, err, "the event definitions document is larger than the 4 MiB limit")
	})
}

func TestParseDefinitionsScalars(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		// Integers: decimal, octal and hexadecimal, any size.
		{yaml: `12`, want: `12`},
		{yaml: `+12`, want: `12`},
		{yaml: `-007`, want: `-7`},
		{yaml: `0o17`, want: `15`},
		{yaml: `0x1F`, want: `31`},
		{yaml: `123456789012345678901234567890`, want: `123456789012345678901234567890`},
		// Floats keep their digits.
		{yaml: `1.50`, want: `1.50`},
		{yaml: `.5`, want: `0.5`},
		{yaml: `-1.`, want: `-1.0`},
		{yaml: `6.02E+23`, want: `6.02E+23`},
		{yaml: `+1.5e-3`, want: `1.5e-3`},
		// Booleans and nulls.
		{yaml: `True`, want: `true`},
		{yaml: `FALSE`, want: `false`},
		{yaml: `~`, want: `null`},
		{yaml: ``, want: `null`},
		// YAML 1.1 forms and timestamps stay strings.
		{yaml: `yes`, want: `"yes"`},
		{yaml: `on`, want: `"on"`},
		{yaml: `1_000`, want: `"1_000"`},
		{yaml: `0b101`, want: `"0b101"`},
		{yaml: `1:30`, want: `"1:30"`},
		{yaml: `2026-10-09`, want: `"2026-10-09"`},
		{yaml: `2026-10-09T16:58:12Z`, want: `"2026-10-09T16:58:12Z"`},
		// Quoted scalars are strings.
		{yaml: `"12"`, want: `"12"`},
		{yaml: `'true'`, want: `"true"`},
		{yaml: `"<b>&</b>"`, want: `"<b>&</b>"`},
		{yaml: `"\x01"`, want: `"\u0001"`},
		// Explicit tags.
		{yaml: `!!str 12`, want: `"12"`},
		{yaml: `!!int "12"`, want: `12`},
		{yaml: `!!float "2.5"`, want: `2.5`},
		{yaml: `!!timestamp 2026-10-09`, want: `"2026-10-09"`},
		{yaml: `!custom value`, want: `"value"`},
	}
	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			defs, err := ParseDefinitions([]byte("a:\n  payload_schema:\n    v: " + tc.yaml + "\n"))
			require.NoError(t, err)
			assert.Equal(t, `{"v":`+tc.want+`}`, string(defs["a"].PayloadSchema))
		})
	}

	for yamlText, want := range map[string]string{
		`.inf`:       `".inf" is infinite or NaN`,
		`.nan`:       `".nan" is infinite or NaN`,
		`!!int abc`:  `"abc" is not a valid !!int value`,
		`!!bool yes`: `"yes" is not a valid !!bool value`,
	} {
		t.Run("error "+yamlText, func(t *testing.T) {
			_, err := ParseDefinitions([]byte("a:\n  payload_schema:\n    v: " + yamlText + "\n"))
			requireProblems(t, err, want)
		})
	}
}

func TestParseDefinitionsKeysAndAliases(t *testing.T) {
	parse := func(t *testing.T, doc string) string {
		t.Helper()
		defs, err := ParseDefinitions([]byte("a:\n  payload_schema:\n" + doc))
		require.NoError(t, err)
		return string(defs["a"].PayloadSchema)
	}
	assert.Equal(t, `{"b":1,"a":2,"c":[3,{"z":1,"y":2}]}`, parse(t, "    b: 1\n    a: 2\n    c: [3, {z: 1, y: 2}]\n"), "document order")
	assert.Equal(t, `{"200":"ok","true":"yes","0x10":"hex"}`, parse(t, "    200: ok\n    true: yes\n    0x10: hex\n"), "keys as written")
	assert.Equal(t, `{"<<":"x"}`, parse(t, "    \"<<\": x\n"), "quoted merge key")
	assert.Equal(t, `{"k":{"x":[1,2]},"l":{"x":[1,2]}}`, parse(t, "    k: &s {x: [1, 2]}\n    l: *s\n"), "aliases expand")
	assert.Equal(t, `{"k":"total","total":1}`, parse(t, "    k: &name total\n    *name : 1\n"), "aliased key")
}

func TestParseDefinitionsNode(t *testing.T) {
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: {description: x}\n"), &doc))
	defs, err := ParseDefinitionsNode(&doc)
	require.NoError(t, err)
	assert.Equal(t, "x", defs["a"].Description)

	_, err = ParseDefinitionsNode(nil)
	requireProblems(t, err, "event definitions must be a mapping")
}

func readTestdata(elem ...string) ([]byte, error) {
	return os.ReadFile(filepath.Join(append([]string{"testdata"}, elem...)...))
}

func TestParseDefinitionsAliasExpansion(t *testing.T) {
	t.Run("billion laughs", func(t *testing.T) {
		data, err := readTestdata("yaml", "billion-laughs.yaml")
		require.NoError(t, err)
		start := time.Now()
		_, err = ParseDefinitions(data)
		requireProblems(t, err, "the schemas are more than 16 MiB as JSON in total")
		assert.Less(t, time.Since(start), 2*time.Second)
	})

	t.Run("alias cycle", func(t *testing.T) {
		data, err := readTestdata("yaml", "alias-cycle.yaml")
		require.NoError(t, err)
		_, err = ParseDefinitions(data)
		requireProblems(t, err, `/order.created/payload_schema/properties/self/properties (line 8): alias *props refers to a node that contains it`)
	})
}

func TestParseDefinitionsAliasBudgetIsPerDocument(t *testing.T) {
	big := `"` + strings.Repeat("x", 1000) + `"`
	var sb strings.Builder
	fmt.Fprintf(&sb, "e0:\n  payload_schema: &l0 [%s]\n", strings.TrimSuffix(strings.Repeat(big+", ", 10), ", "))
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&sb, "e%d:\n  payload_schema: &l%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*l%d, ", i-1), 10), ", "))
	}
	for i := 4; i < 60; i++ {
		fmt.Fprintf(&sb, "e%d:\n  payload_schema: *l3\n", i)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, err := ParseDefinitions([]byte(sb.String()))
	runtime.ReadMemStats(&after)
	var ce *ConfigError
	require.ErrorAs(t, err, &ce)
	require.Len(t, ce.Problems, 1)
	assert.Contains(t, ce.Problems[0], "the schemas are more than 16 MiB as JSON in total")
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(200<<20))
}

func TestParseDefinitionsNestedErrorsKeepOnePrefix(t *testing.T) {
	_, err := ParseDefinitions([]byte("e:\n  payload_schema: {type: object, type: string}\n  bogus: 1\n  description: 3\n"))
	var ce *ConfigError
	require.ErrorAs(t, err, &ce)
	require.Len(t, ce.Problems, 3)
	for _, p := range ce.Problems {
		assert.NotContains(t, p, "invalid mcp events")
		assert.NotContains(t, p, "\n")
	}
}

func TestParseDefinitionsJSON(t *testing.T) {
	schema := func(t *testing.T, doc string) string {
		t.Helper()
		defs, err := ParseDefinitionsJSON([]byte(doc))
		require.NoError(t, err)
		return string(defs["a"].PayloadSchema)
	}
	assert.Equal(t, `{"pattern":"a/b"}`, schema(t, `{"a":{"payload_schema":{"pattern":"a\/b"}}}`), "escaped slash")
	assert.Equal(t, "{\"title\":\"\U0001F600\"}", schema(t, `{"a":{"payload_schema":{"title":"\ud83d\ude00"}}}`), "surrogate pair")
	assert.Equal(t, `{"maximum":1e400,"min":-0.5,"ok":true,"n":null,"s":"true"}`, schema(t, `{"a":{"payload_schema":{"maximum":1e400,"min":-0.5,"ok":true,"n":null,"s":"true"}}}`), "numbers stay numbers")
	assert.Equal(t, `{"b":1,"a":2}`, schema(t, "\ufeff{\"a\":{\"payload_schema\":{\"b\":1,\"a\":2}}}"), "document order, BOM")

	for name, c := range map[string]struct{ doc, want string }{
		"duplicate event":  {`{"a":{},"a":{}}`, `duplicate key "a"`},
		"duplicate nested": {`{"a":{"payload_schema":{"type":"object","type":"string"}}}`, `duplicate key "type"`},
		"unknown field":    {`{"a":{"x":1}}`, `unknown field "x"`},
		"syntax":           {`{"a":`, "invalid JSON"},
		"trailing":         {`{} {}`, "invalid JSON"},
		"empty":            {``, "the definitions document is empty"},
		"not a mapping":    {`[]`, "must be a mapping"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDefinitionsJSON([]byte(c.doc))
			requireProblems(t, err, c.want)
		})
	}
}

package topicschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Set TOPICSCHEMA_UPDATE_GOLDEN=1 to rewrite the golden files.
var parseTestUpdateGolden = os.Getenv("TOPICSCHEMA_UPDATE_GOLDEN") == "1"

func parseTestReadFile(t *testing.T, elem ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, elem...)...))
	require.NoError(t, err)
	return data
}

// parseTestRequireProblems asserts err is a *ConfigError whose problems
// contain each of want, in order.
func parseTestRequireProblems(t *testing.T, err error, want ...string) {
	t.Helper()
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr), "want a *ConfigError, got %T: %v", err, err)
	require.Len(t, cfgErr.Problems, len(want), "problems: %q", cfgErr.Problems)
	for i, w := range want {
		assert.Contains(t, cfgErr.Problems[i], w)
	}
}

func TestParseDefinitionsJSON(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		defs, err := ParseDefinitionsJSON([]byte(`{
			"order.created": {
				"name": "order.created",
				"description": "Fires when <an order> & more.",
				"payload_schema": {
					"type": "object",
					"properties": {
						"zeta": {"type": "integer", "maximum": 9007199254740993},
						"alpha": {"type": "number", "multipleOf": 0.10, "x-mcp-filter": false},
						"note": {"type": "string", "pattern": "^\u00e9\\d+$"}
					},
					"required": ["zeta"]
				},
				"mcp": {"enabled": true}
			},
			"order.deleted": {"description": null, "payload_schema": null, "mcp": null},
			"order.bare": {}
		}`))
		require.NoError(t, err)
		require.Len(t, defs, 3)
		assert.Equal(t, Definition{
			Name:          "order.created",
			Description:   "Fires when <an order> & more.",
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{"zeta":{"type":"integer","maximum":9007199254740993},"alpha":{"type":"number","multipleOf":0.10,"x-mcp-filter":false},"note":{"type":"string","pattern":"^é\\d+$"}},"required":["zeta"]}`),
			MCP:           MCPSettings{Enabled: true},
		}, defs["order.created"])
		assert.Equal(t, Definition{}, defs["order.deleted"])
		assert.Equal(t, Definition{}, defs["order.bare"])
	})

	t.Run("byte order mark", func(t *testing.T) {
		defs, err := ParseDefinitionsJSON([]byte("\xef\xbb\xbf{\"a\": {}}"))
		require.NoError(t, err)
		assert.Contains(t, defs, "a")
	})

	deep := strings.Repeat("[", 130) + strings.Repeat("]", 130)
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "not an object", input: `["order.created"]`, want: []string{"topic schemas must be an object keyed by topic name"}},
		{name: "topic not an object", input: `{"a": 1}`, want: []string{`topic "a": /a: must be an object`}},
		{name: "unknown field", input: `{"a": {"validaton": "warn"}}`, want: []string{`topic "a": /a/validaton: unknown field "validaton"`}},
		{name: "deprecation fields aren't supported", input: `{"a": {"deprecated": true, "replaced_by": "b"}}`, want: []string{
			`topic "a": /a/deprecated: unknown field "deprecated"`,
			`topic "a": /a/replaced_by: unknown field "replaced_by"`,
		}},
		{name: "unknown field hint", input: `{"a": {"payloadSchema": {}}}`, want: []string{`topic "a": /a/payloadSchema: unknown field "payloadSchema" (did you mean "payload_schema"?)`}},
		{name: "unknown mcp field", input: `{"a": {"mcp": {"enable": true}}}`, want: []string{`topic "a": /a/mcp/enable: unknown field "enable"`}},
		{name: "string type", input: `{"a": {"description": 1}}`, want: []string{`topic "a": /a/description: must be a string`}},
		{name: "mcp type", input: `{"a": {"mcp": true}}`, want: []string{`topic "a": /a/mcp: must be an object`}},
		{name: "mcp enabled type", input: `{"a": {"mcp": {"enabled": "true"}}}`, want: []string{`topic "a": /a/mcp/enabled: must be a boolean`}},
		{
			name:  "every problem reported, sorted",
			input: `{"b": {"nmae": "b"}, "a": {"description": [], "mcp": {"on": true}}}`,
			want: []string{
				`topic "a": /a/description: must be a string`,
				`topic "a": /a/mcp/on: unknown field "on"`,
				`topic "b": /b/nmae: unknown field "nmae"`,
			},
		},
		{name: "duplicate topic", input: `{"a": {}, "a": {}}`, want: []string{`duplicate key "a"`}},
		{
			name:  "duplicate key in payload schema",
			input: `{"a": {"payload_schema": {"properties": {"x": {"type": "string", "type": "number"}}}}}`,
			want:  []string{`topic "a": /a/payload_schema/properties/x: duplicate key "type"`},
		},
		{name: "syntax error", input: `{"a": {"name": }}`, want: []string{`topic "a": /a/name: invalid JSON at byte 16`}},
		{name: "truncated", input: `{"a": {`, want: []string{`topic "a": /a: invalid JSON at byte 7`}},
		{name: "trailing data", input: `{} {}`, want: []string{"unexpected data after the JSON document"}},
		{name: "empty", input: ``, want: []string{"the JSON document is empty"}},
		{name: "too deep", input: `{"a": {"payload_schema": {"enum": ` + deep + `}}}`, want: []string{`topic "a": /a/payload_schema/enum/0/0/`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, err := ParseDefinitionsJSON([]byte(tc.input))
			assert.Nil(t, defs)
			parseTestRequireProblems(t, err, tc.want...)
		})
	}

	t.Run("depth limit", func(t *testing.T) {
		// The root, topic and payload_schema objects take three levels.
		nested := func(n int) string {
			return `{"a": {"payload_schema": {"enum": ` + strings.Repeat("[", n) + strings.Repeat("]", n) + `}}}`
		}
		_, err := ParseDefinitionsJSON([]byte(nested(parseMaxDepth - 3)))
		require.NoError(t, err)
		_, err = ParseDefinitionsJSON([]byte(nested(parseMaxDepth - 2)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nesting exceeds 128 levels")
	})

	t.Run("size limit", func(t *testing.T) {
		big := `{"a": {"description": "` + strings.Repeat("x", parseMaxInputBytes) + `"}}`
		_, err := ParseDefinitionsJSON([]byte(big))
		parseTestRequireProblems(t, err, "the topic schemas document is larger than the 4 MiB limit")
	})
}

func TestParseDefinitionsYAML(t *testing.T) {
	t.Run("same as JSON", func(t *testing.T) {
		fromYAML, err := ParseDefinitionsYAML([]byte(`
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
  mcp:
    enabled: true
order.bare: {}
`))
		require.NoError(t, err)
		fromJSON, err := ParseDefinitionsJSON([]byte(`{
			"order.created": {
				"description": "Fires when <an order> & more.",
				"payload_schema": {"type": "object", "properties": {
					"zeta": {"type": "integer", "maximum": 9007199254740993},
					"alpha": {"type": "number", "multipleOf": 0.10, "x-mcp-filter": false},
					"placedOn": {"type": "string", "format": "date", "default": "2026-10-09",
						"examples": ["2026-10-09T16:58:12Z", "2026-10-09 16:58:12.5 -5"]}
				}, "required": ["zeta"]},
				"mcp": {"enabled": true}
			},
			"order.bare": {}
		}`))
		require.NoError(t, err)
		assert.Equal(t, fromJSON, fromYAML)
	})

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unknown field", input: "a:\n  validaton: warn\n", want: `topic "a": /a/validaton: unknown field "validaton"`},
		{name: "duplicate key", input: "a:\n  payload_schema:\n    type: object\n    type: string\n", want: `topic "a": /a/payload_schema (line 4): duplicate key "type"`},
		{name: "duplicate topic", input: "a: {}\na: {}\n", want: `line 2: duplicate key "a"`},
		{name: "merge key", input: "base: &base\n  description: x\na:\n  <<: *base\n", want: `topic "a": /a (line 4): merge keys (<<) are not supported`},
		{name: "null key", input: "~: {}\n", want: `line 1: mapping keys must be strings, not null`},
		{name: "complex key", input: "? [a, b]\n: {}\n", want: `line 1: mapping keys must be strings`},
		{name: "infinity", input: "a:\n  payload_schema:\n    maximum: .inf\n", want: `topic "a": /a/payload_schema/maximum (line 3): ".inf" is infinite or NaN`},
		{name: "not a mapping", input: "- a\n- b\n", want: "topic schemas must be an object keyed by topic name"},
		{name: "empty", input: "# nothing\n", want: "the YAML document is empty"},
		{name: "several documents", input: "a: {}\n---\nb: {}\n", want: "a YAML stream with more than one document is not supported"},
		{name: "invalid", input: "a: [\n", want: "invalid YAML: line 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, err := ParseDefinitionsYAML([]byte(tc.input))
			assert.Nil(t, defs)
			parseTestRequireProblems(t, err, tc.want)
		})
	}

	t.Run("size limit", func(t *testing.T) {
		_, err := ParseDefinitionsYAML([]byte("a:\n  description: " + strings.Repeat("x", parseMaxInputBytes)))
		parseTestRequireProblems(t, err, "the topic schemas document is larger than the 4 MiB limit")
	})
}

func TestYAMLNodeToJSONScalars(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		// Integers: decimal, octal and hexadecimal, any size.
		{yaml: `12`, want: `12`},
		{yaml: `+12`, want: `12`},
		{yaml: `-007`, want: `-7`},
		{yaml: `0`, want: `0`},
		{yaml: `0o17`, want: `15`},
		{yaml: `0x1F`, want: `31`},
		{yaml: `123456789012345678901234567890`, want: `123456789012345678901234567890`},
		// Floats keep their digits.
		{yaml: `1.50`, want: `1.50`},
		{yaml: `.5`, want: `0.5`},
		{yaml: `-1.`, want: `-1.0`},
		{yaml: `1e3`, want: `1e3`},
		{yaml: `6.02E+23`, want: `6.02E+23`},
		{yaml: `+1.5e-3`, want: `1.5e-3`},
		// Booleans and nulls.
		{yaml: `true`, want: `true`},
		{yaml: `True`, want: `true`},
		{yaml: `FALSE`, want: `false`},
		{yaml: `null`, want: `null`},
		{yaml: `Null`, want: `null`},
		{yaml: `~`, want: `null`},
		{yaml: ``, want: `null`},
		// YAML 1.1 forms and timestamps stay strings.
		{yaml: `yes`, want: `"yes"`},
		{yaml: `No`, want: `"No"`},
		{yaml: `on`, want: `"on"`},
		{yaml: `y`, want: `"y"`},
		{yaml: `1_000`, want: `"1_000"`},
		{yaml: `0b101`, want: `"0b101"`},
		{yaml: `1:30`, want: `"1:30"`},
		{yaml: `-0x1F`, want: `"-0x1F"`},
		{yaml: `1.2.3`, want: `"1.2.3"`},
		{yaml: `2026-10-09`, want: `"2026-10-09"`},
		{yaml: `2026-10-09T16:58:12Z`, want: `"2026-10-09T16:58:12Z"`},
		{yaml: `2001-12-14 21:59:43.10 -5`, want: `"2001-12-14 21:59:43.10 -5"`},
		// Quoted and block scalars are strings.
		{yaml: `"12"`, want: `"12"`},
		{yaml: `'true'`, want: `"true"`},
		{yaml: `"null"`, want: `"null"`},
		{yaml: "|\n  line 1\n  line 2", want: `"line 1\nline 2\n"`},
		{yaml: `"<b>&</b>"`, want: `"<b>&</b>"`},
		{yaml: `"tab\there\u2028"`, want: `"tab\there\u2028"`},
		{yaml: `"\x01"`, want: `"\u0001"`},
		// Explicit tags.
		{yaml: `!!str 12`, want: `"12"`},
		{yaml: `!!int "12"`, want: `12`},
		{yaml: `!!float 1`, want: `1`},
		{yaml: `!!float "2.5"`, want: `2.5`},
		{yaml: `!!bool "true"`, want: `true`},
		{yaml: `!!null ""`, want: `null`},
		{yaml: `!!timestamp 2026-10-09`, want: `"2026-10-09"`},
		{yaml: `!!binary aGVsbG8=`, want: `"aGVsbG8="`},
		{yaml: `!custom value`, want: `"value"`},
	}
	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			var n yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte("v: "+tc.yaml+"\n"), &n))
			got, err := YAMLNodeToJSON(&n)
			require.NoError(t, err)
			assert.Equal(t, `{"v":`+tc.want+`}`, string(got))
		})
	}

	errorCases := []struct {
		yaml string
		want string
	}{
		{yaml: `.inf`, want: `/v (line 1): ".inf" is infinite or NaN, which JSON can't represent`},
		{yaml: `-.Inf`, want: `"-.Inf" is infinite`},
		{yaml: `.nan`, want: `".nan" is infinite or NaN`},
		{yaml: `!!int abc`, want: `/v (line 1): "abc" is not a valid !!int value`},
		{yaml: `!!int 1.5`, want: `"1.5" is not a valid !!int value`},
		{yaml: `!!bool yes`, want: `"yes" is not a valid !!bool value`},
		{yaml: `!!null 0`, want: `"0" is not a valid !!null value`},
		{yaml: `!!float .inf`, want: `".inf" is infinite or NaN`},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.yaml, func(t *testing.T) {
			var n yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte("v: "+tc.yaml+"\n"), &n))
			_, err := YAMLNodeToJSON(&n)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestYAMLNodeToJSON(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
		err  string
	}{
		{name: "key order", yaml: "b: 1\na: 2\nc: [3, {z: 1, y: 2}]\n", want: `{"b":1,"a":2,"c":[3,{"z":1,"y":2}]}`},
		{name: "non-string keys", yaml: "200: ok\ntrue: yes\n1.5: x\n0x10: hex\n", want: `{"200":"ok","true":"yes","1.5":"x","0x10":"hex"}`},
		{name: "quoted merge key", yaml: "\"<<\": x\n", want: `{"<<":"x"}`},
		{name: "aliases", yaml: "a: &x {k: [1, 2]}\nb: *x\nc: [*x]\n", want: `{"a":{"k":[1,2]},"b":{"k":[1,2]},"c":[{"k":[1,2]}]}`},
		{name: "aliased key", yaml: "k: &name total\n*name : 1\n", want: `{"k":"total","total":1}`},
		{name: "scalar document", yaml: "42\n", want: `42`},
		{name: "duplicate key", yaml: "a:\n  b: 1\n  b: 2\n", err: `/a (line 3): duplicate key "b"`},
		{name: "duplicate key after many", yaml: "{" + parseTestKeys(40) + ", k7: 1}\n", err: `duplicate key "k7"`},
		{name: "merge key", yaml: "a: &a {x: 1}\nb:\n  <<: *a\n", err: `/b (line 3): merge keys (<<) are not supported`},
		{name: "null key", yaml: "null: x\n", err: "mapping keys must be strings, not null"},
		{name: "sequence key", yaml: "? [a]\n: x\n", err: "mapping keys must be strings"},
		{name: "alias cycle", yaml: "a: &x [1, *x]\n", err: `/a/1 (line 1): alias *x refers to a node that contains it`},
		{name: "mapping alias cycle", yaml: "a: &x\n  b:\n    c: *x\n", err: `/a/b/c (line 3): alias *x refers to a node that contains it`},
		{name: "deep but allowed", yaml: "a: " + strings.Repeat("[", 127) + strings.Repeat("]", 127) + "\n", want: `{"a":` + strings.Repeat("[", 127) + strings.Repeat("]", 127) + `}`},
		{name: "too deep", yaml: "a: " + strings.Repeat("[", 128) + strings.Repeat("]", 128) + "\n", err: "nesting exceeds 128 levels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var n yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &n))
			got, err := YAMLNodeToJSON(&n)
			if tc.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
			assert.True(t, json.Valid(got))
		})
	}

	t.Run("nil and empty nodes", func(t *testing.T) {
		_, err := YAMLNodeToJSON(nil)
		require.Error(t, err)
		got, err := YAMLNodeToJSON(&yaml.Node{Kind: yaml.DocumentNode})
		require.NoError(t, err)
		assert.Equal(t, "null", string(got))
		_, err = YAMLNodeToJSON(&yaml.Node{})
		require.Error(t, err)
	})

	t.Run("programmatic string node", func(t *testing.T) {
		n := &yaml.Node{}
		n.SetString("123")
		got, err := YAMLNodeToJSON(n)
		require.NoError(t, err)
		assert.Equal(t, `"123"`, string(got))
	})
}

func TestParseAppendJSONString(t *testing.T) {
	// Byte escapes: U+2028, U+2029, invalid UTF-8, a truncated sequence and
	// an emoji.
	for _, s := range []string{
		"",
		"plain",
		`quote " and backslash \`,
		"\b\f\n\r\t\x00\x1f\x7f",
		"<a href='x'>&amp;</a>",
		"caf\xc3\xa9 \xe6\x97\xa5\xe6\x9c\xac",
		"line\xe2\x80\xa8para\xe2\x80\xa9end",
		"bad \xff\xfe bytes",
		"truncated \xc3",
		"\xf0\x9f\x98\x80",
	} {
		want, err := marshalNoEscape(s)
		require.NoError(t, err)
		assert.Equal(t, string(want), string(parseAppendJSONString(nil, s)), "%q", s)
	}
}

func parseTestKeys(n int) string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d: %d", i, i)
	}
	return strings.Join(keys, ", ")
}

func TestYAMLAliasBudget(t *testing.T) {
	t.Run("billion laughs", func(t *testing.T) {
		data := parseTestReadFile(t, "yaml", "billion-laughs.yaml")
		require.Less(t, len(data), 1024)

		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		start := time.Now()
		_, err := ParseDefinitionsYAML(data)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)

		parseTestRequireProblems(t, err, `topic "order.created": /order.created/payload_schema/`)
		assert.Contains(t, err.Error(), "the document expands to more than 1000000 nodes")
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("rejected in %s after allocating %d KiB", elapsed, allocated>>10)
		assert.Less(t, elapsed, 2*time.Second)
		assert.Less(t, allocated, uint64(64<<20))
	})

	t.Run("alias cycle", func(t *testing.T) {
		_, err := ParseDefinitionsYAML(parseTestReadFile(t, "yaml", "alias-cycle.yaml"))
		parseTestRequireProblems(t, err, `topic "order.created": /order.created/payload_schema/properties/self/properties (line 8): alias *props refers to a node that contains it`)
	})

	t.Run("output size", func(t *testing.T) {
		// 20 aliases of a 1 MiB string: few nodes, but 20 MiB of output.
		var sb strings.Builder
		sb.WriteString("s: &s \"" + strings.Repeat("x", 1<<20) + "\"\nl: [")
		for i := range 20 {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("*s")
		}
		sb.WriteString("]\n")
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(sb.String()), &n))
		_, err := YAMLNodeToJSON(&n)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the document expands to more than 16 MiB of JSON")
	})

	t.Run("within budget", func(t *testing.T) {
		// 9^5 = 59049 scalars through nested aliases is fine.
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(`
a: &a [1, 1, 1, 1, 1, 1, 1, 1, 1]
b: &b [*a, *a, *a, *a, *a, *a, *a, *a, *a]
c: &c [*b, *b, *b, *b, *b, *b, *b, *b, *b]
d: &d [*c, *c, *c, *c, *c, *c, *c, *c, *c]
e: [*d, *d, *d, *d, *d, *d, *d, *d, *d]
`), &n))
		got, err := YAMLNodeToJSON(&n)
		require.NoError(t, err)
		var v map[string]any
		require.NoError(t, json.Unmarshal(got, &v))
	})
}

func TestParseNodeBudget(t *testing.T) {
	zeros := func(n int) string { return strings.TrimSuffix(strings.Repeat("0,", n), ",") }
	// allocated returns the bytes f allocates.
	allocated := func(f func()) uint64 {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		f()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	t.Run("JSON", func(t *testing.T) {
		// The root and the array are values too.
		_, err := ParseDefinitionsJSON([]byte(`{"a":[` + zeros(parseMaxNodes-2) + `]}`))
		parseTestRequireProblems(t, err, `topic "a": /a: must be an object`)

		_, err = ParseDefinitionsJSON([]byte(`{"a":[` + zeros(parseMaxNodes-1) + `]}`))
		parseTestRequireProblems(t, err, "the JSON document has more than 1000000 values")
	})

	t.Run("YAML is checked before it is decoded", func(t *testing.T) {
		// A flow sequence of 500k items would take yaml.v3 about 100 MiB.
		data := []byte("a: [" + zeros(parseMaxNodes/2) + "]\n")
		var err error
		alloc := allocated(func() { _, err = ParseDefinitionsYAML(data) })
		parseTestRequireProblems(t, err, "the YAML document may have more than 1000000 nodes")
		assert.Less(t, alloc, uint64(1<<20))
	})

	t.Run("without aliases the converter doesn't blame them", func(t *testing.T) {
		item := &yaml.Node{Kind: yaml.ScalarNode, Value: "0"}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Content: slices.Repeat([]*yaml.Node{item}, parseMaxNodes)}
		_, err := YAMLNodeToJSON(seq)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the document has more than 1000000 nodes")
		assert.NotContains(t, err.Error(), "alias")
	})
}

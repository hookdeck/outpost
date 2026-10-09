package topicschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
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
				"validation": "enforce",
				"mcp": {"enabled": true},
				"deprecated": true,
				"replaced_by": "order.created.v2"
			},
			"order.deleted": {"description": null, "payload_schema": null, "mcp": null, "deprecated": null},
			"order.bare": {}
		}`))
		require.NoError(t, err)
		require.Len(t, defs, 3)
		assert.Equal(t, Definition{
			Name:          "order.created",
			Description:   "Fires when <an order> & more.",
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{"zeta":{"type":"integer","maximum":9007199254740993},"alpha":{"type":"number","multipleOf":0.10,"x-mcp-filter":false},"note":{"type":"string","pattern":"^é\\d+$"}},"required":["zeta"]}`),
			Validation:    ValidationEnforce,
			MCP:           MCPSettings{Enabled: true},
			Deprecated:    true,
			ReplacedBy:    "order.created.v2",
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
		{name: "unknown field hint", input: `{"a": {"payloadSchema": {}}}`, want: []string{`topic "a": /a/payloadSchema: unknown field "payloadSchema" (did you mean "payload_schema"?)`}},
		{name: "unknown mcp field", input: `{"a": {"mcp": {"enable": true}}}`, want: []string{`topic "a": /a/mcp/enable: unknown field "enable"`}},
		{name: "string type", input: `{"a": {"validation": 1}}`, want: []string{`topic "a": /a/validation: must be a string`}},
		{name: "bool type", input: `{"a": {"deprecated": "yes"}}`, want: []string{`topic "a": /a/deprecated: must be a boolean`}},
		{name: "mcp type", input: `{"a": {"mcp": true}}`, want: []string{`topic "a": /a/mcp: must be an object`}},
		{name: "mcp enabled type", input: `{"a": {"mcp": {"enabled": "true"}}}`, want: []string{`topic "a": /a/mcp/enabled: must be a boolean`}},
		{
			name:  "every problem reported, sorted",
			input: `{"b": {"nmae": "b"}, "a": {"validation": [], "mcp": {"on": true}}}`,
			want: []string{
				`topic "a": /a/mcp/on: unknown field "on"`,
				`topic "a": /a/validation: must be a string`,
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
  validation: enforce
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
				"validation": "enforce",
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

func TestParseOpenAPIGolden(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		golden  string
	}{
		{name: "components", sources: []string{"components.yaml", "components.json"}, golden: "components.golden.json"},
		{name: "recursive", sources: []string{"recursive.yaml"}, golden: "recursive.golden.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var first Definitions
			for _, source := range tc.sources {
				defs, err := ParseOpenAPI(parseTestReadFile(t, "openapi", source))
				require.NoError(t, err, source)
				if first == nil {
					first = defs
					continue
				}
				assert.Equal(t, first, defs, "%s must import like %s", source, tc.sources[0])
			}
			got, err := json.MarshalIndent(first, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')
			path := filepath.Join("testdata", "openapi", tc.golden)
			if parseTestUpdateGolden {
				require.NoError(t, os.WriteFile(path, got, 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got), "run with TOPICSCHEMA_UPDATE_GOLDEN=1 to update")
		})
	}
}

// parseTestNoLoader refuses every remote resource, as the catalog compiler
// does, so a bundled reference that escaped its schema fails to compile.
type parseTestNoLoader struct{}

func (parseTestNoLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("loading %s is not allowed", url)
}

func parseTestCompile(t *testing.T, schema json.RawMessage) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(parseTestNoLoader{})
	require.NoError(t, c.AddResource("mem://payload.json", doc))
	sch, err := c.Compile("mem://payload.json")
	require.NoError(t, err, "%s", schema)
	return sch
}

func parseTestValid(t *testing.T, sch *jsonschema.Schema, instance string) bool {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(instance))
	require.NoError(t, err)
	return sch.Validate(v) == nil
}

// TestParseOpenAPIBundledSchemasValidate compiles the bundled payload
// schemas with no access to the OpenAPI document and checks that the
// components they reference still apply.
func TestParseOpenAPIBundledSchemasValidate(t *testing.T) {
	components, err := ParseOpenAPI(parseTestReadFile(t, "openapi", "components.yaml"))
	require.NoError(t, err)
	recursive, err := ParseOpenAPI(parseTestReadFile(t, "openapi", "recursive.yaml"))
	require.NoError(t, err)
	for _, defs := range []Definitions{components, recursive} {
		for topic, def := range defs {
			t.Run("compiles "+topic, func(t *testing.T) {
				parseTestCompile(t, def.PayloadSchema)
			})
		}
	}

	order := `{"orderId": "ord_1", "status": "paid", "total": {"amount": 1200, "currency": "USD"},
		"customer": {"id": "cus_1", "shippingAddress": {"country": "CA"}},
		"items": [{"sku": "A", "quantity": 1, "unitPrice": {"amount": 1200, "currency": "USD"}}],
		"metadata": {"gift": true}}`
	cases := []struct {
		name     string
		schema   json.RawMessage
		instance string
		valid    bool
	}{
		{name: "order", schema: components["order.created"].PayloadSchema, instance: order, valid: true},
		{name: "order bad currency", schema: components["order.created"].PayloadSchema, instance: strings.Replace(order, `"currency": "USD"}`, `"currency": "GBP"}`, 1), valid: false},
		{name: "order bad nested address", schema: components["order.created"].PayloadSchema, instance: strings.Replace(order, `"CA"`, `"Canada"`, 1), valid: false},
		{name: "order bad item price", schema: components["order.created"].PayloadSchema, instance: strings.Replace(order, `"unitPrice": {"amount": 1200, "currency": "USD"}`, `"unitPrice": {"amount": -1, "currency": "USD"}`, 1), valid: false},
		{name: "order bad metadata", schema: components["order.created"].PayloadSchema, instance: strings.Replace(order, `{"gift": true}`, `{"gift": [true]}`, 1), valid: false},
		{name: "update", schema: components["order.updated"].PayloadSchema, instance: `{"order": ` + order + `, "changedFields": ["status"], "displayTotal": "$12.00", "previousTotal": {"amount": 1, "currency": "EUR"}}`, valid: true},
		{name: "update local $defs", schema: components["order.updated"].PayloadSchema, instance: `{"order": ` + order + `, "changedFields": ["status"], "displayTotal": 12}`, valid: false},
		{name: "update allOf", schema: components["order.updated"].PayloadSchema, instance: `{"order": {"reason": "x"}, "changedFields": []}`, valid: false},
		{name: "refund tail ref", schema: components["refund.issued"].PayloadSchema, instance: `{"refundId": "r", "orderId": "o", "amount": {"amount": 1, "currency": "CAD"}, "lineItemSku": 5}`, valid: false},
		{name: "payment card", schema: components["payment.succeeded"].PayloadSchema, instance: `{"paymentId": "p", "amount": {"amount": 1, "currency": "CAD"}, "method": {"kind": "card", "last4": "4242"}}`, valid: true},
		{name: "payment bad card", schema: components["payment.succeeded"].PayloadSchema, instance: `{"paymentId": "p", "amount": {"amount": 1, "currency": "CAD"}, "method": {"kind": "card", "last4": "42"}}`, valid: false},
		{name: "category", schema: recursive["category.updated"].PayloadSchema, instance: `{"name": "a", "parent": {"name": "b", "parent": {"name": "c"}}, "children": [{"name": "d"}]}`, valid: true},
		{name: "category deep invalid", schema: recursive["category.updated"].PayloadSchema, instance: `{"name": "a", "parent": {"name": "b", "children": [{"parent": {}}]}}`, valid: false},
		{name: "tree", schema: recursive["tree.changed"].PayloadSchema, instance: `{"tree": {"value": "a", "branches": [{"label": "l", "node": {"value": "b"}}]}, "parentCategory": {"name": "x"}}`, valid: true},
		{name: "tree deep invalid", schema: recursive["tree.changed"].PayloadSchema, instance: `{"tree": {"branches": [{"node": {"branches": [{"label": 1}]}}]}}`, valid: false},
		{name: "tree tail ref", schema: recursive["tree.changed"].PayloadSchema, instance: `{"parentCategory": {"parent": {}}}`, valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEmpty(t, tc.schema)
			assert.Equal(t, tc.valid, parseTestValid(t, parseTestCompile(t, tc.schema), tc.instance))
		})
	}
}

// parseTestOpenAPI wraps webhooks and components YAML in a 3.1 document.
func parseTestOpenAPI(webhooks, components string) []byte {
	doc := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\nwebhooks:\n" + parseTestIndent(webhooks)
	if components != "" {
		doc += "components:\n" + parseTestIndent(components)
	}
	return []byte(doc)
}

func parseTestIndent(s string) string {
	lines := strings.Split(strings.Trim(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// parseTestJSONBody is a webhook POSTing schema as application/json.
func parseTestJSONBody(key, schema string) string {
	return key + ":\n  post:\n    requestBody:\n      content:\n        application/json:\n          schema: " + schema + "\n"
}

func TestParseOpenAPI(t *testing.T) {
	cases := []struct {
		name       string
		webhooks   string
		components string
		// want maps topic to its payload schema; every other webhook must be
		// skipped.
		want map[string]string
	}{
		{
			name: "application/json preferred over an earlier +json",
			webhooks: `
a:
  post:
    requestBody:
      content:
        application/vnd.acme+json: {schema: {const: 1}}
        application/json: {schema: {const: 2}}
`,
			want: map[string]string{"a": `{"const":2}`},
		},
		{
			name: "first +json fallback",
			webhooks: `
a:
  post:
    requestBody:
      content:
        text/plain: {schema: {const: 1}}
        application/cloudevents+json: {schema: {const: 2}}
        application/problem+json: {schema: {const: 3}}
`,
			want: map[string]string{"a": `{"const":2}`},
		},
		{
			name: "skipped webhooks",
			webhooks: `
noPost:
  put:
    requestBody: {content: {application/json: {schema: {type: object}}}}
noBody:
  post: {summary: s}
noContent:
  post: {requestBody: {description: d}}
noSchema:
  post: {requestBody: {content: {application/json: {example: {}}}}}
xml:
  post: {requestBody: {content: {application/xml: {schema: {type: object}}}}}
`,
			want: map[string]string{},
		},
		{
			name:     "boolean schema",
			webhooks: parseTestJSONBody("a", "true"),
			want:     map[string]string{"a": `true`},
		},
		{
			name: "operation, request body and media type refs",
			webhooks: `
a:
  post:
    $ref: '#/components/x-operations/Create'
`,
			components: `
x-operations:
  Create:
    requestBody:
      $ref: '#/components/requestBodies/Create'
      description: overrides the referenced description
requestBodies:
  Create:
    description: d
    content:
      application/json:
        $ref: '#/components/x-media/CreateJSON'
x-media:
  CreateJSON:
    schema: {type: object, properties: {id: {$ref: '#/components/schemas/Id'}}}
schemas:
  Id: {type: string, minLength: 1}
`,
			want: map[string]string{"a": `{"type":"object","properties":{"id":{"$ref":"#/$defs/Id"}},"$defs":{"Id":{"type":"string","minLength":1}}}`},
		},
		{
			name:     "self reference through the document pointer",
			webhooks: parseTestJSONBody("a", `{type: object, properties: {child: {$ref: '#/webhooks/a/post/requestBody/content/application~1json/schema'}, name: {$ref: '#/webhooks/a/post/requestBody/content/application~1json/schema/properties/child'}}}`),
			want:     map[string]string{"a": `{"type":"object","properties":{"child":{"$ref":"#"},"name":{"$ref":"#/properties/child"}}}`},
		},
		{
			name:     "schema-local references and anchors are kept",
			webhooks: parseTestJSONBody("a", `{$defs: {n: {$anchor: node, type: string}}, properties: {x: {$ref: '#/$defs/n'}, y: {$ref: '#node'}, z: {$ref: '#'}}}`),
			want:     map[string]string{"a": `{"$defs":{"n":{"$anchor":"node","type":"string"}},"properties":{"x":{"$ref":"#/$defs/n"},"y":{"$ref":"#node"},"z":{"$ref":"#"}}}`},
		},
		{
			name:       "escaped and percent-encoded pointers",
			webhooks:   parseTestJSONBody("a", `{properties: {x: {$ref: '#/components/schemas/Map/properties/a~1b'}, y: {$ref: '#/components/schemas/Map/properties/c%20d'}}}`),
			components: "schemas:\n  Map: {properties: {a/b: {const: 1}, c d: {const: 2}}}\n",
			want:       map[string]string{"a": `{"properties":{"x":{"$ref":"#/$defs/Map/properties/a~1b"},"y":{"$ref":"#/$defs/Map/properties/c%20d"}},"$defs":{"Map":{"properties":{"a/b":{"const":1},"c d":{"const":2}}}}}`},
		},
		{
			name:       "name collisions with existing $defs",
			webhooks:   parseTestJSONBody("a", `{$defs: {Money: {const: local}, Money_2: {const: local2}}, properties: {a: {$ref: '#/components/schemas/Money'}, b: {$ref: '#/components/schemas/Money_2'}, c: {$ref: '#/$defs/Money_2'}}}`),
			components: "schemas:\n  Money: {const: m}\n  Money_2: {const: m2}\n",
			want:       map[string]string{"a": `{"$defs":{"Money":{"const":"local"},"Money_2":{"const":"local2"},"Money_3":{"const":"m"},"Money_2_2":{"const":"m2"}},"properties":{"a":{"$ref":"#/$defs/Money_3"},"b":{"$ref":"#/$defs/Money_2_2"},"c":{"$ref":"#/$defs/Money_2"}}}`},
		},
		{
			name:     "refs in every subschema position",
			webhooks: parseTestJSONBody("a", `{$ref: '#/components/schemas/Root'}`),
			components: `
schemas:
  Root:
    allOf: [{$ref: '#/components/schemas/A'}]
    anyOf: [{$ref: '#/components/schemas/B'}]
    oneOf: [{$ref: '#/components/schemas/C'}]
    not: {$ref: '#/components/schemas/D'}
    if: {$ref: '#/components/schemas/E'}
    then: {$ref: '#/components/schemas/F'}
    else: {$ref: '#/components/schemas/G'}
    items: {$ref: '#/components/schemas/H'}
    prefixItems: [{$ref: '#/components/schemas/I'}]
    additionalProperties: {$ref: '#/components/schemas/J'}
    patternProperties: {'^x': {$ref: '#/components/schemas/K'}}
    dependentSchemas: {k: {$ref: '#/components/schemas/L'}}
    propertyNames: {$ref: '#/components/schemas/M'}
    contains: {$ref: '#/components/schemas/N'}
    unevaluatedProperties: {$ref: '#/components/schemas/O'}
    properties: {p: {$dynamicRef: '#/components/schemas/P'}}
    enum: [{$ref: '#/components/schemas/Q'}]
    x-extension: {$ref: '#/components/schemas/R'}
  A: {}
  B: {}
  C: {}
  D: {}
  E: {}
  F: {}
  G: {}
  H: {}
  I: {}
  J: {}
  K: {}
  L: {}
  M: {}
  N: {}
  O: {}
  P: {}
`,
			want: map[string]string{"a": `{` +
				`"allOf":[{"$ref":"#/$defs/A"}],"anyOf":[{"$ref":"#/$defs/B"}],"oneOf":[{"$ref":"#/$defs/C"}],` +
				`"not":{"$ref":"#/$defs/D"},"if":{"$ref":"#/$defs/E"},"then":{"$ref":"#/$defs/F"},"else":{"$ref":"#/$defs/G"},` +
				`"items":{"$ref":"#/$defs/H"},"prefixItems":[{"$ref":"#/$defs/I"}],"additionalProperties":{"$ref":"#/$defs/J"},` +
				`"patternProperties":{"^x":{"$ref":"#/$defs/K"}},"dependentSchemas":{"k":{"$ref":"#/$defs/L"}},` +
				`"propertyNames":{"$ref":"#/$defs/M"},"contains":{"$ref":"#/$defs/N"},"unevaluatedProperties":{"$ref":"#/$defs/O"},` +
				`"properties":{"p":{"$dynamicRef":"#/$defs/P"}},` +
				// Data and extensions are not schemas: their refs are kept.
				`"enum":[{"$ref":"#/components/schemas/Q"}],"x-extension":{"$ref":"#/components/schemas/R"},` +
				`"$defs":{"A":{},"B":{},"C":{},"D":{},"E":{},"F":{},"G":{},"H":{},"I":{},"J":{},"K":{},"L":{},"M":{},"N":{},"O":{},"P":{}}` +
				`}`},
		},
		{
			name:       "root ref with constraints next to it is not inlined",
			webhooks:   parseTestJSONBody("a", `{$ref: '#/components/schemas/Order', required: [id]}`),
			components: "schemas:\n  Order: {type: object}\n",
			want:       map[string]string{"a": `{"$ref":"#/$defs/Order","required":["id"],"$defs":{"Order":{"type":"object"}}}`},
		},
		{
			name:       "root ref chain",
			webhooks:   parseTestJSONBody("a", `{$ref: '#/components/schemas/Alias', title: Payload}`),
			components: "schemas:\n  Alias: {$ref: '#/components/schemas/Order', description: aliased}\n  Order: {type: object, title: Order, properties: {self: {$ref: '#/components/schemas/Order'}, alias: {$ref: '#/components/schemas/Alias'}}}\n",
			want:       map[string]string{"a": `{"type":"object","title":"Payload","properties":{"self":{"$ref":"#"},"alias":{"$ref":"#/$defs/Alias"}},"description":"aliased","$defs":{"Alias":{"$ref":"#","description":"aliased"}}}`},
		},
		{
			name:       "$id at the root",
			webhooks:   parseTestJSONBody("a", `{$id: 'https://example.com/a', properties: {b: {$ref: '#/components/schemas/B'}}}`),
			components: "schemas:\n  B: {type: string}\n",
			want:       map[string]string{"a": `{"$id":"https://example.com/a","properties":{"b":{"$ref":"#/$defs/B"}},"$defs":{"B":{"type":"string"}}}`},
		},
		{
			name: "topic names and descriptions",
			webhooks: `
k1:
  x-outpost-topic: from.path.item
  post:
    summary: ''
    description: Falls back to the description.
    requestBody: {content: {application/json: {schema: {const: 1}}}}
k2:
  x-outpost-topic: ignored
  post:
    x-outpost-topic: from.operation
    summary: Summary wins.
    description: d
    requestBody: {content: {application/json: {schema: {const: 2}}}}
k3:
  $ref: '#/components/pathItems/Shared'
  x-outpost-topic: from.reference
`,
			components: "pathItems:\n  Shared:\n    x-outpost-topic: shared\n    post: {requestBody: {content: {application/json: {schema: {const: 3}}}}}\n",
			want:       map[string]string{"from.path.item": `{"const":1}`, "from.operation": `{"const":2}`, "from.reference": `{"const":3}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, err := ParseOpenAPI(parseTestOpenAPI(tc.webhooks, tc.components))
			require.NoError(t, err)
			got := map[string]string{}
			for topic, def := range defs {
				got[topic] = string(def.PayloadSchema)
			}
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("definition fields", func(t *testing.T) {
		defs, err := ParseOpenAPI(parseTestOpenAPI(`
k1:
  x-outpost-topic: from.path.item
  post:
    description: Falls back to the description.
    requestBody: {content: {application/json: {schema: {const: 1}}}}
k2:
  post:
    summary: Summary wins.
    description: d
    x-mcp-enabled: true
    deprecated: true
    requestBody: {content: {application/json: {schema: {type: object}}}}
`, ""))
		require.NoError(t, err)
		assert.Equal(t, Definitions{
			"from.path.item": {Description: "Falls back to the description.", PayloadSchema: json.RawMessage(`{"const":1}`)},
			"k2":             {Description: "Summary wins.", PayloadSchema: json.RawMessage(`{"type":"object"}`), MCP: MCPSettings{Enabled: true}, Deprecated: true},
		}, defs)
	})

	t.Run("JSON document", func(t *testing.T) {
		defs, err := ParseOpenAPI([]byte(`{"openapi": "3.1.2", "webhooks": {"a": {"post": {"requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/A"}}}}}}}, "components": {"schemas": {"A": {"type": "object", "properties": {"n": {"type": "integer", "maximum": 1e400}}}}}}`))
		require.NoError(t, err)
		assert.Equal(t, `{"type":"object","properties":{"n":{"type":"integer","maximum":1e400}}}`, string(defs["a"].PayloadSchema))
	})

	t.Run("no webhooks", func(t *testing.T) {
		defs, err := ParseOpenAPI([]byte("openapi: 3.1.0\nwebhooks: {}\n"))
		require.NoError(t, err)
		assert.Empty(t, defs)
	})
}

func TestParseOpenAPIErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  []byte
		want []string
	}{
		{
			name: "OpenAPI 3.0",
			doc:  parseTestReadFile(t, "openapi", "openapi30.yaml"),
			want: []string{`/openapi: version "3.0.3" is not supported; only OpenAPI 3.1.x documents can be imported`},
		},
		{name: "OpenAPI 3.2", doc: []byte("openapi: 3.2.0\nwebhooks: {}\n"), want: []string{`/openapi: version "3.2.0" is not supported`}},
		{name: "Swagger 2.0", doc: []byte(`{"swagger": "2.0", "paths": {}}`), want: []string{"/swagger: Swagger 2.0 documents are not supported"}},
		{name: "missing version", doc: []byte("webhooks: {}\n"), want: []string{"/openapi: missing; only OpenAPI 3.1.x documents can be imported"}},
		{name: "numeric version", doc: []byte("openapi: 3.1\nwebhooks: {}\n"), want: []string{`/openapi: must be a version string such as "3.1.0"`}},
		{
			name: "external ref",
			doc:  parseTestReadFile(t, "openapi", "external-ref.yaml"),
			want: []string{`topic "order.created": /components/schemas/Order/properties/customer/$ref: external $ref "./schemas/customer.yaml#/Customer" is not supported`},
		},
		{
			name: "external ref at the root",
			doc:  parseTestOpenAPI(parseTestJSONBody("a", `{$ref: 'https://example.com/a.json'}`), ""),
			want: []string{`topic "a": /webhooks/a/post/requestBody/content/application~1json/schema/$ref: external $ref "https://example.com/a.json" is not supported`},
		},
		{
			name: "external path item ref",
			doc:  parseTestOpenAPI("a:\n  $ref: 'hooks.yaml#/a'\n", ""),
			want: []string{`topic "a": /webhooks/a/$ref: external $ref "hooks.yaml#/a" is not supported`},
		},
		{
			name: "missing component",
			doc:  parseTestOpenAPI(parseTestJSONBody("a", `{properties: {b: {$ref: '#/components/schemas/Nope'}}}`), ""),
			want: []string{`topic "a": /webhooks/a/post/requestBody/content/application~1json/schema/properties/b/$ref: $ref "#/components/schemas/Nope" does not resolve`},
		},
		{
			name: "missing component member",
			doc:  parseTestOpenAPI(parseTestJSONBody("a", `{$ref: '#/components/schemas/A/properties/nope'}`), "schemas:\n  A: {properties: {}}\n"),
			want: []string{`$ref "#/components/schemas/A/properties/nope" does not resolve`},
		},
		{
			name: "reference to another part of the document",
			doc:  parseTestOpenAPI(parseTestJSONBody("a", `{$ref: '#/components/schemas/A'}`), "schemas:\n  A: {properties: {p: {$ref: '#/components/parameters/P/schema'}}}\nparameters:\n  P: {schema: {type: string}}\n"),
			want: []string{`topic "a": /components/schemas/A/properties/p/$ref: unsupported $ref "#/components/parameters/P/schema": only #/components/schemas references can be bundled`},
		},
		{
			name: "nested $id",
			doc:  parseTestOpenAPI(parseTestJSONBody("a", `{properties: {b: {$ref: '#/components/schemas/B'}}}`), "schemas:\n  B: {$id: 'https://example.com/b', type: string}\n"),
			want: []string{`topic "a": /components/schemas/B/$id: $id is only supported at the payload schema root`},
		},
		{
			name: "missing request body",
			doc:  parseTestOpenAPI("a:\n  post:\n    requestBody: {$ref: '#/components/requestBodies/Nope'}\n", ""),
			want: []string{`topic "a": /webhooks/a/post/requestBody/$ref: $ref "#/components/requestBodies/Nope" does not resolve`},
		},
		{
			name: "circular path items",
			doc:  parseTestOpenAPI("a:\n  $ref: '#/components/pathItems/A'\n", "pathItems:\n  A: {$ref: '#/components/pathItems/B'}\n  B: {$ref: '#/components/pathItems/A'}\n"),
			want: []string{`topic "a": /components/pathItems/B/$ref: circular $ref "#/components/pathItems/A"`},
		},
		{
			name: "MCP opt-in without a JSON body",
			doc:  parseTestOpenAPI("a:\n  post:\n    x-mcp-enabled: true\n    requestBody: {content: {text/plain: {schema: {type: string}}}}\n", ""),
			want: []string{`topic "a": /webhooks/a/post/x-mcp-enabled: needs a request body with a JSON schema`},
		},
		{
			name: "wrong field types",
			doc: parseTestOpenAPI(`
a:
  post:
    x-mcp-enabled: "true"
    requestBody: {content: {application/json: {schema: {}}}}
b:
  post:
    summary: [s]
c:
  post:
    deprecated: 1
d:
  x-outpost-topic: ''
  post: {}
e:
  post: []
f: []
g:
  post:
    requestBody: {content: []}
`, ""),
			want: []string{
				`topic "a": /webhooks/a/post/x-mcp-enabled: must be a boolean`,
				`topic "b": /webhooks/b/post/summary: must be a string`,
				`topic "c": /webhooks/c/post/deprecated: must be a boolean`,
				`topic "d": /webhooks/d/x-outpost-topic: must be a non-empty string`,
				`topic "e": /webhooks/e/post: must be an operation object`,
				`topic "f": /webhooks/f: must be a path item object`,
				`topic "g": /webhooks/g/post/requestBody/content: must be an object`,
			},
		},
		{
			name: "duplicate topic",
			doc:  parseTestOpenAPI(parseTestJSONBody("a", "{}")+"b:\n  x-outpost-topic: a\n  post: {requestBody: {content: {application/json: {schema: {}}}}}\n", ""),
			want: []string{`topic "a": /webhooks/b: duplicate topic, also defined by /webhooks/a`},
		},
		{name: "no webhooks", doc: []byte("openapi: 3.1.0\npaths: {}\n"), want: []string{"/webhooks: the document defines no webhooks to import"}},
		{name: "webhooks not an object", doc: []byte("openapi: 3.1.0\nwebhooks: []\n"), want: []string{"/webhooks: must be an object"}},
		{name: "not an object", doc: []byte("- openapi\n"), want: []string{"the OpenAPI document must be an object"}},
		{name: "invalid JSON", doc: []byte(`{"openapi": "3.1.0",}`), want: []string{"invalid JSON at byte 20"}},
		{name: "duplicate JSON key", doc: []byte(`{"openapi": "3.1.0", "webhooks": {"a": {}, "a": {}}}`), want: []string{`/webhooks: duplicate key "a"`}},
		{name: "invalid YAML", doc: []byte("openapi: 3.1.0\nwebhooks: [\n"), want: []string{"invalid YAML"}},
		{name: "YAML alias bomb", doc: append([]byte("openapi: 3.1.0\n"), parseTestReadFile(t, "yaml", "billion-laughs.yaml")...), want: []string{"the document expands to more than 1000000 nodes"}},
		{name: "empty", doc: []byte("\n"), want: []string{"the YAML document is empty"}},
		{name: "size limit", doc: []byte("openapi: 3.1.0\ninfo:\n  description: " + strings.Repeat("x", parseMaxInputBytes) + "\n"), want: []string{"the OpenAPI document is larger than the 4 MiB limit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, err := ParseOpenAPI(tc.doc)
			assert.Nil(t, defs)
			parseTestRequireProblems(t, err, tc.want...)
		})
	}

	t.Run("bundled size limit", func(t *testing.T) {
		// One ~200 KiB component referenced by 400 webhooks: a small document
		// whose bundled payload schemas would take about 80 MiB.
		values := make([]string, 20000)
		for i := range values {
			values[i] = fmt.Sprintf("value-%05d", i)
		}
		var hooks strings.Builder
		for i := range 400 {
			hooks.WriteString(parseTestJSONBody(fmt.Sprintf("w%d", i), `{$ref: '#/components/schemas/Big'}`))
		}
		doc := parseTestOpenAPI(hooks.String(), "schemas:\n  Big: {enum: ["+strings.Join(values, ", ")+"]}\n")
		require.Less(t, len(doc), parseMaxInputBytes)
		_, err := ParseOpenAPI(doc)
		parseTestRequireProblems(t, err, "the payload schemas bundled from the OpenAPI document exceed 64 MiB")
	})
}

func TestMerge(t *testing.T) {
	base := Definitions{
		"a": {Description: "base a", Validation: ValidationWarn, PayloadSchema: json.RawMessage(`{"type":"object"}`)},
		"b": {Description: "base b"},
	}
	override := Definitions{
		"b": {Description: "override b"},
		"c": {Description: "override c"},
	}
	got := Merge(base, override)
	assert.Equal(t, Definitions{
		"a": {Description: "base a", Validation: ValidationWarn, PayloadSchema: json.RawMessage(`{"type":"object"}`)},
		// Replaced whole: no payload schema or validation carried over.
		"b": {Description: "override b"},
		"c": {Description: "override c"},
	}, got)
	assert.Len(t, base, 2, "base is not modified")
	assert.Len(t, override, 2, "override is not modified")
	assert.Equal(t, "base b", base["b"].Description)

	assert.Empty(t, Merge(nil, nil))
	assert.NotNil(t, Merge(nil, nil))
	assert.Equal(t, override, Merge(nil, override))
}

func BenchmarkParseOpenAPI(b *testing.B) {
	for _, source := range []string{"components.yaml", "components.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "openapi", source))
		require.NoError(b, err)
		b.Run(source, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := ParseOpenAPI(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

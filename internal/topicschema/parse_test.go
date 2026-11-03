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

const (
	emptyInput = `{"type":"object","properties":{}}`
	objPayload = `{"type":"object"}`
)

func TestParseDefinitions(t *testing.T) {
	const asYAML = `
# Comments are fine.
resultType: complete
nextCursor: null
events:
  - name: order.created
    description: Fires when <an order> & more.
    delivery: [webhook, poll]
    _meta: {owner: billing}
    inputSchema:
      type: object
      properties:
        zeta: {type: integer}
    payloadSchema:
      type: object
      properties:
        zeta: {type: integer, maximum: 9007199254740993}
        placedOn:
          type: string
          default: 2026-10-09
  - name: order.bare
    inputSchema: null
`
	const asJSON = `{"resultType":"complete","events":[
	{"name":"order.created","description":"Fires when <an order> & more.","delivery":["webhook","poll"],"_meta":{"owner":"billing"},
	 "inputSchema":{"type":"object","properties":{"zeta":{"type":"integer"}}},
	 "payloadSchema":{"type":"object","properties":{"zeta":{"type":"integer","maximum":9007199254740993},"placedOn":{"type":"string","default":"2026-10-09"}}}},
	{"name":"order.bare"}]}`

	for name, parse := range map[string]func([]byte) (Definitions, error){
		"yaml": ParseDefinitions,
		"json": ParseDefinitionsJSON,
	} {
		doc := asYAML
		if name == "json" {
			doc = asJSON
		}
		t.Run(name, func(t *testing.T) {
			defs, err := parse([]byte(doc))
			require.NoError(t, err)
			require.Len(t, defs, 2)
			d := defs[0]
			assert.Equal(t, "order.created", d.Name)
			assert.Equal(t, "Fires when <an order> & more.", d.Description)
			assert.Equal(t, []string{"webhook", "poll"}, d.Delivery)
			assert.JSONEq(t, `{"owner":"billing"}`, string(d.Meta))
			assert.Equal(t, `{"type":"object","properties":{"zeta":{"type":"integer"}}}`, string(d.InputSchema))
			assert.Equal(t, `{"type":"object","properties":{"zeta":{"type":"integer","maximum":9007199254740993},"placedOn":{"type":"string","default":"2026-10-09"}}}`, string(d.PayloadSchema))
			assert.Equal(t, "order.bare", defs[1].Name)
			assert.Empty(t, defs[1].InputSchema)
		})
	}
}

func TestParseDefinitionsProblems(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{"not an object", `[]`, `must be an object with an "events" list`},
		{"no events", `{}`, `must be an object with an "events" list`},
		{"events not a list", `{"events":{}}`, `must be an object with an "events" list`},
		{"top-level field", `{"events":[],"foo":1}`, `unknown field "foo"`},
		{"resultType", `{"events":[],"resultType":"incomplete"}`, `resultType must be "complete"`},
		{"nextCursor", `{"events":[],"nextCursor":"abc"}`, "nextCursor is set: the list must be complete; merge every page into events"},
		{"item not an object", `{"events":[1]}`, "events[0]: must be an object"},
		{"name missing", `{"events":[{}]}`, "events[0]: name is required"},
		{"name empty", `{"events":[{"name":""}]}`, "events[0]: name must be a non-empty string"},
		{"name number", `{"events":[{"name":1}]}`, "events[0]: name must be a non-empty string"},
		{"unknown field", `{"events":[{"name":"a","foo":1}]}`, `event "a": unknown field "foo"`},
		{"suggestion", `{"events":[{"name":"a","payload_schema":{}}]}`, `unknown field "payload_schema" (did you mean "payloadSchema"?)`},
		{"description", `{"events":[{"name":"a","description":3}]}`, `event "a": description must be a string`},
		{"delivery empty", `{"events":[{"name":"a","delivery":[]}]}`, `delivery must be a non-empty list of "poll", "push" and "webhook"`},
		{"delivery unknown", `{"events":[{"name":"a","delivery":["webhook","smoke"]}]}`, `delivery must be a non-empty list`},
		{"delivery duplicate", `{"events":[{"name":"a","delivery":["webhook","webhook"]}]}`, `delivery must be a non-empty list`},
		{"delivery without webhook", `{"events":[{"name":"a","delivery":["poll"]}]}`, `delivery must include "webhook"; Outpost delivers by webhook only`},
		{"meta", `{"events":[{"name":"a","_meta":[]}]}`, `event "a": _meta must be an object`},
		{"duplicate key", `{"events":[{"name":"a","inputSchema":{"type":"object","type":"string"}}]}`, `duplicate key "type"`},
		{"syntax", `{"events":`, "invalid JSON"},
		{"empty", ``, "the definitions document is empty"},
		{"trailing", `{} {}`, "invalid JSON"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseDefinitionsJSON([]byte(c.doc))
			requireProblems(t, err, c.want)
		})
	}
}

func TestParseDefinitionsKeysAndAliases(t *testing.T) {
	parse := func(t *testing.T, doc string) string {
		t.Helper()
		defs, err := ParseDefinitions([]byte("events:\n  - name: a\n    payloadSchema:\n" + doc))
		require.NoError(t, err)
		return string(defs[0].PayloadSchema)
	}
	assert.Equal(t, `{"b":1,"a":2,"c":[3,{"z":1,"y":2}]}`, parse(t, "      b: 1\n      a: 2\n      c: [3, {z: 1, y: 2}]\n"), "document order")
	assert.Equal(t, `{"200":"ok","true":"yes","0x10":"hex"}`, parse(t, "      200: ok\n      true: yes\n      0x10: hex\n"), "keys as written")
	assert.Equal(t, `{"k":{"x":[1,2]},"l":{"x":[1,2]}}`, parse(t, "      k: &s {x: [1, 2]}\n      l: *s\n"), "aliases expand")
}

func TestParseDefinitionsJSONScalars(t *testing.T) {
	schema := func(t *testing.T, doc string) string {
		t.Helper()
		defs, err := ParseDefinitionsJSON([]byte(doc))
		require.NoError(t, err)
		return string(defs[0].PayloadSchema)
	}
	assert.Equal(t, `{"pattern":"a/b"}`, schema(t, `{"events":[{"name":"a","payloadSchema":{"pattern":"a\/b"}}]}`), "escaped slash")
	assert.Equal(t, `{"maximum":1e400,"min":-0.5,"ok":true,"n":null,"s":"true"}`, schema(t, `{"events":[{"name":"a","payloadSchema":{"maximum":1e400,"min":-0.5,"ok":true,"n":null,"s":"true"}}]}`), "numbers stay numbers")
	assert.Equal(t, `{"b":1,"a":2}`, schema(t, "\ufeff{\"events\":[{\"name\":\"a\",\"payloadSchema\":{\"b\":1,\"a\":2}}]}"), "document order, BOM")
}

func TestParseDefinitionsNode(t *testing.T) {
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("events: [{name: x, description: d}]\n"), &doc))
	defs, err := ParseDefinitionsNode(&doc)
	require.NoError(t, err)
	assert.Equal(t, "d", defs[0].Description)

	_, err = ParseDefinitionsNode(nil)
	requireProblems(t, err, `must be an object with an "events" list`)
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
		requireProblems(t, err, `/payloadSchema/properties/self/properties (line 9): alias *props refers to a node that contains it`)
	})
}

func TestParseDefinitionsAliasBudgetIsPerDocument(t *testing.T) {
	big := `"` + strings.Repeat("x", 1000) + `"`
	var sb strings.Builder
	sb.WriteString("events:\n")
	fmt.Fprintf(&sb, "  - name: e0\n    payloadSchema: &l0 [%s]\n", strings.TrimSuffix(strings.Repeat(big+", ", 10), ", "))
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&sb, "  - name: e%d\n    payloadSchema: &l%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*l%d, ", i-1), 10), ", "))
	}
	for i := 4; i < 60; i++ {
		fmt.Fprintf(&sb, "  - name: e%d\n    payloadSchema: *l3\n", i)
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

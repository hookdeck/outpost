package mcpcatalog

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileSchema(t *testing.T, doc map[string]any) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	require.NoError(t, c.AddResource("mem:///s.json", doc))
	s, err := c.Compile("mem:///s.json")
	require.NoError(t, err)
	return s
}

func roundTrip(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

func TestSampleCases(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	obj := func(s string) map[string]any {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(s), &m))
		return m
	}
	cases := map[string]struct {
		schema string
		want   func(i int) any
	}{
		"const":      {`{"const":"x"}`, func(int) any { return "x" }},
		"enum cycle": {`{"enum":["a","b","c"]}`, func(i int) any { return []any{"a", "b", "c"}[i%3] }},
		"examples":   {`{"type":"string","examples":["p","q"]}`, func(i int) any { return []any{"p", "q"}[i%2] }},
		"default":    {`{"type":"string","default":"d"}`, func(int) any { return "d" }},
		"oneOf":      {`{"oneOf":[{"const":1},{"const":2}]}`, func(i int) any { return float64(1 + i%2) }},
		"bool":       {`{"type":"boolean"}`, func(i int) any { return i%2 == 0 }},
		"null":       {`{"type":"null"}`, func(int) any { return nil }},
	}
	for name, c := range cases {
		for i := 0; i < 3; i++ {
			got := roundTrip(t, Sample(obj(c.schema), i, now))
			assert.Equal(t, c.want(i), got, "%s i=%d", name, i)
		}
	}

	schema := obj(`{
	  "type":"object",
	  "properties":{
	    "when":{"type":"string","format":"date-time"},
	    "day":{"type":"string","format":"date"},
	    "mail":{"type":"string","format":"email"},
	    "link":{"type":"string","format":"uri"},
	    "id":{"type":"string","format":"uuid"},
	    "short":{"type":"string","maxLength":3},
	    "long":{"type":"string","minLength":12},
	    "n":{"type":"integer","minimum":10,"maximum":12},
	    "x":{"type":"number"},
	    "tags":{"type":"array","items":{"type":"string"},"minItems":2,"maxItems":2},
	    "child":{"$ref":"#/$defs/node"},
	    "merged":{"allOf":[{"properties":{"a":{"type":"string"}}},{"properties":{"b":{"type":"integer"}}}]},
	    "maybe":{"type":["null","string"]}
	  },
	  "$defs":{"node":{"type":"object","properties":{"name":{"type":"string"},"next":{"$ref":"#/$defs/node"}}}}
	}`)
	s := compileSchema(t, schema)
	var prev any
	for i := 0; i < 5; i++ {
		v := roundTrip(t, Sample(schema, i, now))
		require.NoError(t, s.Validate(v), "i=%d: %v", i, v)
		assert.Equal(t, v, roundTrip(t, Sample(schema, i, now)), "deterministic")
		assert.NotEqual(t, prev, v, "differs by i")
		prev = v
		m := v.(map[string]any)
		assert.LessOrEqual(t, len(m["short"].(string)), 3)
		assert.GreaterOrEqual(t, len(m["long"].(string)), 12)
		assert.Len(t, m["tags"], 2)
		assert.Contains(t, m["merged"], "a")
		assert.Contains(t, m["merged"], "b")
		assert.NotContains(t, m["child"].(map[string]any), "next", "a $ref cycle is cut")
	}
	assert.Equal(t, "2026-01-02T03:04:05Z", roundTrip(t, Sample(schema, 0, now)).(map[string]any)["when"])
}

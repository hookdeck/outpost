package mcpcheck

import (
	"testing"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func event(props map[string]any, examples ...any) mcpclient.Event {
	ev := mcpclient.Event{Name: "e", InputSchema: map[string]any{"type": "object", "properties": props}}
	if len(examples) > 0 {
		ev.PayloadSchema = map[string]any{"examples": examples}
	}
	return ev
}

func TestFilterCaseByType(t *testing.T) {
	for _, c := range []struct {
		name         string
		prop         map[string]any
		match, other any
	}{
		{"enum", map[string]any{"type": "string", "enum": []any{"P1", "P2", "P3"}}, "P1", "P2"},
		{"string", map[string]any{"type": "string"}, "mcp-check", "mcp-check-other"},
		{"number", map[string]any{"type": "number"}, float64(1), float64(2)},
		{"integer", map[string]any{"type": "integer"}, float64(1), float64(2)},
		{"boolean", map[string]any{"type": "boolean"}, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			res, err := FilterCase(event(map[string]any{"f": c.prop}), nil, nil, nil)
			require.NoError(t, err)
			assert.Equal(t, "f", res.Field)
			assert.Equal(t, map[string]any{"f": c.match}, res.Match)
			assert.Equal(t, map[string]any{"f": c.other}, res.Other)
			assert.Equal(t, map[string]any{"f": c.match}, res.Data)
			assert.Equal(t, map[string]any{"f": c.other}, res.OtherData)
		})
	}
}

func TestFilterCasePicksFirstArgumentByName(t *testing.T) {
	res, err := FilterCase(event(map[string]any{
		"zeta":  map[string]any{"type": "string"},
		"alpha": map[string]any{"type": "object"},
		"beta":  map[string]any{"type": "boolean"},
	}), nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "beta", res.Field)
}

func TestFilterCasePrefersEnumArguments(t *testing.T) {
	res, err := FilterCase(event(map[string]any{
		"a": map[string]any{"type": "string"},
		"z": map[string]any{"type": "string", "enum": []any{"x", "y"}},
	}), nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "z", res.Field)
}

func TestFilterCaseWithoutUsableArgument(t *testing.T) {
	for name, props := range map[string]map[string]any{
		"none":   {},
		"const":  {"f": map[string]any{"const": "x"}},
		"single": {"f": map[string]any{"enum": []any{"x"}}},
		"object": {"f": map[string]any{"type": "object"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := FilterCase(event(props), nil, nil, nil)
			assert.ErrorIs(t, err, ErrNoArgument)
			assert.False(t, HasFilterArgument(event(props)))
		})
	}
}

func TestFilterCaseGivenArguments(t *testing.T) {
	ev := event(map[string]any{"severity": map[string]any{"type": "string", "enum": []any{"P1", "P2", "P3"}}})
	res, err := FilterCase(ev, map[string]any{"severity": "P2"}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "severity", res.Field)
	assert.Equal(t, map[string]any{"severity": "P1"}, res.Other)

	res, err = FilterCase(event(nil), map[string]any{"n": float64(5), "a": "x"}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "a", res.Field)
	assert.Equal(t, map[string]any{"n": float64(5), "a": "x-other"}, res.Other)

	res, err = FilterCase(event(nil), map[string]any{"o": map[string]any{}}, nil, nil)
	assert.Error(t, err)
	assert.Equal(t, map[string]any{"o": map[string]any{}}, res.Data)
}

func TestFilterCasePayloadStartsFromExample(t *testing.T) {
	ev := event(map[string]any{"status": map[string]any{"enum": []any{"paid", "pending"}}},
		map[string]any{"id": "o1", "status": "ignored"})
	res, err := FilterCase(ev, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"id": "o1", "status": "paid"}, res.Data)
	assert.Equal(t, map[string]any{"id": "o1", "status": "pending"}, res.OtherData)
}

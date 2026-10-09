package mcpevents

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The testdata/*.json vectors were generated with node 24 from the MCP Events
// guide's reference implementation (canonicalJson and deriveSubscriptionId,
// copied verbatim) and WHATWG `new URL(input).href`, by testdata/gen.mjs.

func loadVectors[T any](t *testing.T, name string) []T {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	var out []T
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotEmpty(t, out)
	return out
}

func TestCanonicalJSON_GoldenVectors(t *testing.T) {
	t.Parallel()
	type vector struct {
		Name      string `json:"name"`
		Input     string `json:"input"`
		Canonical string `json:"canonical"`
	}
	for _, v := range loadVectors[vector](t, "canonical_json.json") {
		t.Run(v.Name, func(t *testing.T) {
			t.Parallel()
			// As decoded by encoding/json (float64 numbers).
			var decoded any
			require.NoError(t, json.Unmarshal([]byte(v.Input), &decoded))
			got, err := CanonicalJSON(decoded)
			require.NoError(t, err)
			assert.Equal(t, v.Canonical, string(got))

			// As decoded by the strict decoder (json.Number).
			got, err = CanonicalizeJSON([]byte(v.Input))
			require.NoError(t, err)
			assert.Equal(t, v.Canonical, string(got))

			// From a json.RawMessage.
			got, err = CanonicalJSON(json.RawMessage(v.Input))
			require.NoError(t, err)
			assert.Equal(t, v.Canonical, string(got))
		})
	}
}

func TestDeriveSubscriptionID_GoldenVectors(t *testing.T) {
	t.Parallel()
	type vector struct {
		Principal    string `json:"principal"`
		URL          string `json:"url"`
		Name         string `json:"name"`
		Arguments    string `json:"arguments"`
		CanonicalKey string `json:"canonical_key"`
		ID           string `json:"id"`
	}
	for _, v := range loadVectors[vector](t, "subscription_ids.json") {
		args, err := CanonicalizeJSON([]byte(v.Arguments))
		require.NoError(t, err)
		assert.Equal(t, v.ID, DeriveSubscriptionID(v.Principal, v.URL, v.Name, args), "principal %q", v.Principal)

		var parsedArgs any
		require.NoError(t, json.Unmarshal([]byte(v.Arguments), &parsedArgs))
		key, err := CanonicalJSON(map[string]any{"principal": v.Principal, "url": v.URL, "name": v.Name, "arguments": parsedArgs})
		require.NoError(t, err)
		assert.Equal(t, v.CanonicalKey, string(key))
	}
}

func TestDeriveSubscriptionID(t *testing.T) {
	t.Parallel()
	id := DeriveSubscriptionID("p", "https://a.example/", "t", json.RawMessage(`{}`))
	assert.Equal(t, id, DeriveSubscriptionID("p", "https://a.example/", "t", nil), "nil arguments mean {}")
	assert.Len(t, id, len("sub_")+32)
	assert.True(t, strings.HasPrefix(id, "sub_"))
	// Every key component matters.
	for _, other := range []string{
		DeriveSubscriptionID("q", "https://a.example/", "t", nil),
		DeriveSubscriptionID("p", "https://b.example/", "t", nil),
		DeriveSubscriptionID("p", "https://a.example/", "u", nil),
		DeriveSubscriptionID("p", "https://a.example/", "t", json.RawMessage(`{"a":1}`)),
	} {
		assert.NotEqual(t, id, other)
	}
	// Components can't run into each other: a quote in the principal is
	// escaped, not a field boundary.
	assert.NotEqual(t,
		DeriveSubscriptionID(`a","url":"x`, "y", "t", nil),
		DeriveSubscriptionID("a", "x", "t", nil))
}

// Intentional differences from the guide's canonicalJson. Inputs here come
// from Go's JSON decoding, so they can't reach the JavaScript-only cases:
//   - lone surrogate escapes ("\ud800"): JSON.parse keeps them and
//     JSON.stringify writes "\ud800"; encoding/json decodes them to U+FFFD,
//     written raw. Invalid UTF-8 bytes become U+FFFD one per byte, as
//     encoding/json does (a WHATWG decoder replaces maximal subparts).
//   - numbers beyond float64 range (1e400): JavaScript reads Infinity and
//     writes null; here they are an error (invalid_params for arguments).
//   - duplicate keys: JSON.parse keeps the last; CanonicalizeJSON rejects
//     them, since another parser could keep the first.
func TestCanonicalJSON_IntentionalDifferences(t *testing.T) {
	t.Parallel()

	got, err := CanonicalizeJSON([]byte(`"\ud800"`))
	require.NoError(t, err)
	assert.Equal(t, "\"�\"", string(got))

	got, err = CanonicalJSON("a\xffb\xc3")
	require.NoError(t, err)
	assert.Equal(t, "\"a�b�\"", string(got))

	_, err = CanonicalizeJSON([]byte(`1e400`))
	assert.Error(t, err)
	_, err = CanonicalizeJSON([]byte(`{"a":-1e400}`))
	assert.Error(t, err)

	_, err = CanonicalizeJSON([]byte(`{"a":1,"a":2}`))
	assert.ErrorIs(t, err, errDuplicateKey)
	_, err = CanonicalizeJSON([]byte(`{"x":{"a":1,"b":{},"a":2}}`))
	assert.ErrorIs(t, err, errDuplicateKey)
}

func TestCanonicalJSON_Types(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"int", 42, "42"},
		{"int64 beyond 2^53 rounds like JavaScript", int64(9007199254740993), "9007199254740992"},
		{"uint32", uint32(7), "7"},
		{"float32", float32(0.5), "0.5"},
		{"json.Number", json.Number("1E2"), "100"},
		{"negative zero", math.Copysign(0, -1), "0"},
		{"nested typed map falls back to encoding/json", map[string]string{"b": "<&>", "a": " "}, "{\"a\":\" \",\"b\":\"<&>\"}"},
		{"struct falls back to encoding/json", struct {
			B int    `json:"b"`
			A string `json:"a"`
		}{1, "x"}, `{"a":"x","b":1}`},
		{"nil slice element", []any{nil, true, false}, "[null,true,false]"},
		{"empty map", map[string]any{}, "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalJSON(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestCanonicalJSON_Errors(t *testing.T) {
	t.Parallel()
	for _, v := range []any{math.NaN(), math.Inf(1), math.Inf(-1), []any{math.NaN()}, map[string]any{"a": math.Inf(1)}, make(chan int)} {
		_, err := CanonicalJSON(v)
		assert.Error(t, err, "%v", v)
	}

	// Keys that only collide after invalid UTF-8 is replaced.
	_, err := CanonicalJSON(map[string]any{"a\xff": 1, "a\xfe": 2})
	assert.Error(t, err)

	// A self-referencing value hits the depth limit instead of the stack.
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err = CanonicalJSON(cyclic)
	assert.ErrorIs(t, err, errCanonicalDepth)

	// Deep JSON input is rejected by the strict decoder.
	deep := strings.Repeat("[", maxCanonicalDepth+1) + strings.Repeat("]", maxCanonicalDepth+1)
	_, err = CanonicalizeJSON([]byte(deep))
	assert.ErrorIs(t, err, errCanonicalDepth)

	for _, bad := range []string{``, `{`, `{"a":}`, `{"a":1} x`, `{"a":1}{}`, `[1,]`, `{1:2}`, `nul`} {
		_, err := CanonicalizeJSON([]byte(bad))
		assert.Error(t, err, "%q", bad)
	}
}

func TestCompareUTF16(t *testing.T) {
	t.Parallel()
	// JavaScript sort order: by UTF-16 code unit, so a surrogate pair
	// (U+1F600, lead unit 0xD83D) sorts before U+FF01 although its code
	// point is higher.
	keys := []string{"！", "\U0001F600", "é", "e", "", " ", "z", "ea", "\U0001F601", "\U00010000"}
	slices.SortFunc(keys, compareUTF16)
	assert.Equal(t, []string{"", "e", "ea", "z", "é", " ", "\U00010000", "\U0001F600", "\U0001F601", "！"}, keys)
	assert.Zero(t, compareUTF16("abc", "abc"))
	assert.Negative(t, compareUTF16("ab", "abc"))
	assert.Positive(t, compareUTF16("b", "abc"))
}

func TestAppendESNumber(t *testing.T) {
	t.Parallel()
	tests := map[float64]string{
		1:                       "1",
		-1.5:                    "-1.5",
		1e21:                    "1e+21",
		1e-7:                    "1e-7",
		-1e-7:                   "-1e-7",
		1e-6:                    "0.000001",
		9.99999999999999e-7:     "9.99999999999999e-7",
		123456789012345680000.0: "123456789012345680000",
		1e100:                   "1e+100",
		5e-324:                  "5e-324",
		math.MaxFloat64:         "1.7976931348623157e+308",
		0.30000000000000004:     "0.30000000000000004",
	}
	for in, want := range tests {
		got, err := appendESNumber(nil, in)
		require.NoError(t, err)
		assert.Equal(t, want, string(got), "%v", in)
	}
}

func TestAppendJSString(t *testing.T) {
	t.Parallel()
	got := appendJSString(nil, "\"\\/\b\f\n\r\t\x00\x1f\x7f<>&'  \U0001F600")
	assert.Equal(t, `"\"\\/\b\f\n\r\t\u0000\u001f`+"\x7f<>&'  \U0001F600"+`"`, string(got))
	// Output is valid JSON that decodes back to the input.
	var back string
	require.NoError(t, json.Unmarshal(got, &back))
	assert.Equal(t, "\"\\/\b\f\n\r\t\x00\x1f\x7f<>&'  \U0001F600", back)
}

func FuzzCanonicalizeJSON(f *testing.F) {
	for _, seed := range []string{`{}`, `{"b":1,"a":[1,2,{"d":null,"c":"x"}]}`, `" "`, `-0`, `1e21`, `[1e-7,0.1]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		out, err := CanonicalizeJSON(in)
		if err != nil {
			return
		}
		if !json.Valid(out) {
			t.Fatalf("invalid output %q for %q", out, in)
		}
		// Canonical form is a fixed point.
		again, err := CanonicalizeJSON(out)
		if err != nil || string(again) != string(out) {
			t.Fatalf("not idempotent: %q -> %q (%v)", out, again, err)
		}
	})
}

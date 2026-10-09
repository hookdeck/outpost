package topicschema

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateAgainst compiles schema and validates data with it, as ValidateData
// does.
func validateAgainst(t *testing.T, schema, data string) []string {
	t.Helper()
	tree, err := decodeJSON([]byte(schema))
	require.NoError(t, err)
	compiled, unsupported, err := compileSchema(tree)
	require.NoError(t, err)
	require.Empty(t, unsupported)
	return validateJSON(compiled, []byte(data), "data", propertyNameSet(tree))
}

func TestRenderValidationErrors(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		data   string
		want   []string
	}{
		{"valid", `{"type":"object"}`, `{}`, nil},
		{"type", `{"type":"string"}`, `5`, []string{"data: must be string"}},
		{"nullable type", `{"type":["string","null"]}`, `5`, []string{"data: must be null or string"}},
		{"required", `{"required":["b","a"]}`, `{}`, []string{
			`data: missing required property "a"`,
			`data: missing required property "b"`,
		}},
		{"minimum", `{"minimum":10}`, `5`, []string{"data: must be >= 10"}},
		{"maximum", `{"maximum":0.5}`, `5`, []string{"data: must be <= 0.5"}},
		{"exclusiveMinimum", `{"exclusiveMinimum":1.25}`, `1`, []string{"data: must be > 1.25"}},
		{"exclusiveMaximum", `{"exclusiveMaximum":-3}`, `1`, []string{"data: must be < -3"}},
		{"multipleOf", `{"multipleOf":0.01}`, `1.005`, []string{"data: must be a multiple of 0.01"}},
		{"minLength", `{"minLength":3}`, `"ab"`, []string{"data: must be at least 3 characters"}},
		{"minLength one", `{"minLength":1}`, `""`, []string{"data: must be at least 1 character"}},
		{"maxLength", `{"maxLength":2}`, `"abc"`, []string{"data: must be at most 2 characters"}},
		{"minItems", `{"minItems":2}`, `[1]`, []string{"data: must have at least 2 items"}},
		{"maxItems", `{"maxItems":1}`, `[1,2]`, []string{"data: must have at most 1 item"}},
		{"minProperties", `{"minProperties":1}`, `{}`, []string{"data: must have at least 1 property"}},
		{"maxProperties", `{"maxProperties":1}`, `{"a":1,"b":2}`, []string{"data: must have at most 1 property"}},
		{"enum", `{"enum":["a","b"]}`, `"c"`, []string{"data: must be one of the allowed values"}},
		{"const", `{"const":"a"}`, `"c"`, []string{"data: must equal the allowed value"}},
		{"pattern", `{"pattern":"^a"}`, `"b"`, []string{"data: must match the required pattern"}},
		{"format", `{"format":"date-time"}`, `"nope"`, []string{"data: must be a valid date-time"}},
		{"additionalProperties", `{"properties":{"a":true},"additionalProperties":false}`, `{"a":1,"b":2}`, []string{
			"data: has properties that are not allowed",
		}},
		{"uniqueItems", `{"uniqueItems":true}`, `[1,1]`, []string{`data: does not satisfy "uniqueItems"`}},
		{"not", `{"not":{"type":"string"}}`, `"a"`, []string{`data: does not satisfy "not"`}},
		{"false schema", `{"properties":{"a":false}}`, `{"a":1}`, []string{"data.a: is not allowed"}},
		{"contains", `{"contains":{"type":"string"}}`, `[1,2]`, []string{`data: does not satisfy "contains"`}},
		{"minContains", `{"contains":{"type":"string"},"minContains":2}`, `["a",1]`, []string{`data: does not satisfy "minContains"`}},
		{"propertyNames", `{"propertyNames":{"maxLength":3}}`, `{"toolong":1}`, []string{`data: does not satisfy "propertyNames"`}},
		{"dependentRequired", `{"dependentRequired":{"a":["b"]}}`, `{"a":1}`, []string{`data: does not satisfy "dependentRequired"`}},
		{"oneOf matching twice", `{"oneOf":[{"type":"number"},{"minimum":0}]}`, `1`, []string{`data: does not satisfy "oneOf"`}},
		{"anyOf renders every branch", `{"anyOf":[{"type":"string"},{"type":"number","minimum":10}]}`, `true`, []string{
			"data: must be number",
			"data: must be string",
		}},
		{"anyOf dedupes identical lines", `{"anyOf":[{"type":"string"},{"type":"string","minLength":1}]}`, `5`, []string{"data: must be string"}},
		{"$ref", `{"$ref":"#/$defs/a","$defs":{"a":{"properties":{"b":{"type":"string"}}}}}`, `{"b":1}`, []string{"data.b: must be string"}},
		{"nested path", `{"properties":{"items":{"items":{"properties":{"sku":{"type":"string"}}}}}}`, `{"items":[{"sku":"a"},{"sku":"b"},{"sku":3}]}`, []string{
			"data.items[2].sku: must be string",
		}},
		{"additionalProperties key", `{"additionalProperties":{"type":"string"}}`, `{"secret":1}`, []string{"data.*: must be string"}},
		{"patternProperties key", `{"patternProperties":{"^x":{"type":"string"}}}`, `{"xsecret":1}`, []string{"data.*: must be string"}},
		{"unevaluatedProperties key", `{"unevaluatedProperties":false}`, `{"secret":1}`, []string{"data.*: is not allowed"}},
		{"key named elsewhere in the schema", `{"properties":{"a":{"type":"string"}},"additionalProperties":{"properties":{"a":{"type":"string"}}}}`, `{"zz":{"a":1}}`, []string{
			"data.*.a: must be string",
		}},
		{"key needing quotes", `{"properties":{"a b":{"type":"string"},"$x":{"type":"string"}}}`, `{"a b":1,"$x":1}`, []string{
			"data.$x: must be string",
			`data["a b"]: must be string`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, validateAgainst(t, tc.schema, tc.data))
		})
	}
}

func TestRenderValidationErrorsCap(t *testing.T) {
	items := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "1"
		}
		return "[" + strings.Join(parts, ",") + "]"
	}

	got := validateAgainst(t, `{"items":{"type":"string"}}`, items(25))
	require.Len(t, got, maxReportedErrors+1)
	assert.Equal(t, "... and 5 more", got[maxReportedErrors])
	for _, line := range got[:maxReportedErrors] {
		assert.Regexp(t, `^data\[\d+\]: must be string$`, line)
	}

	got = validateAgainst(t, `{"items":{"type":"string"}}`, items(maxReportedErrors))
	assert.Len(t, got, maxReportedErrors, "no summary when nothing is left out")

	// Past maxCountedErrors nodes the rest isn't counted.
	got = validateAgainst(t, `{"items":{"type":"string"}}`, items(maxCountedErrors+100))
	require.Len(t, got, maxReportedErrors+1)
	assert.Equal(t, "... and more", got[maxReportedErrors])

	// Missing required properties count one each.
	required := make([]string, 30)
	for i := range required {
		required[i] = fmt.Sprintf(`"p%02d"`, i)
	}
	got = validateAgainst(t, `{"required":[`+strings.Join(required, ",")+`]}`, `{}`)
	require.Len(t, got, maxReportedErrors+1)
	assert.Equal(t, "... and 10 more", got[maxReportedErrors])
}

func TestRatString(t *testing.T) {
	for in, want := range map[string]string{
		"100":       "100",
		"-3":        "-3",
		"0.1":       "0.1",
		"1.25":      "1.25",
		"1e-7":      "0.0000001",
		"2.50":      "2.5",
		"1e400":     "1e+400",
		"1.5e-400":  "1.5e-400",
		"123456789": "123456789",
	} {
		r, ok := new(big.Rat).SetString(in)
		require.True(t, ok, in)
		assert.Equal(t, want, ratString(r), in)
	}
	assert.Equal(t, "?", ratString(nil))
}

func TestInstancePath(t *testing.T) {
	instance := map[string]any{
		"items": []any{map[string]any{"sku": "x"}},
		"meta":  map[string]any{"secret key": "x"},
	}
	names := map[string]struct{}{"items": {}, "sku": {}, "meta": {}}
	assert.Equal(t, "data.items[0].sku", instancePath("data", []string{"items", "0", "sku"}, instance, names))
	assert.Equal(t, "data.meta.*", instancePath("data", []string{"meta", "secret key"}, instance, names))
	assert.Equal(t, `data.meta["secret key"]`, instancePath("data", []string{"meta", "secret key"}, instance, nil))
	// Tokens that don't match the instance never render as written.
	assert.Equal(t, "data.items[*].*", instancePath("data", []string{"items", "secret", "sku"}, instance, names))
	assert.Equal(t, "data.items[0].sku.*", instancePath("data", []string{"items", "0", "sku", "secret"}, instance, nil))
	assert.Equal(t, "data", instancePath("data", nil, instance, names))
}

package topicschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const evolutionTopic = "order.created"

func schemaSnapshot(schema string) Snapshot {
	return Snapshot{Topics: map[string]SnapshotTopic{
		evolutionTopic: {MCPEnabled: true, PayloadSchema: json.RawMessage(schema)},
	}}
}

// breaking diffs two payload schemas of one topic and renders the changes as
// "[path] kind: detail".
func breaking(prev, next string) []string {
	return renderChanges(BreakingChanges(schemaSnapshot(prev), schemaSnapshot(next), []string{evolutionTopic}))
}

func renderChanges(changes []Change) []string {
	var out []string
	for _, c := range changes {
		out = append(out, fmt.Sprintf("[%s] %s: %s", c.Path, c.Kind, c.Detail))
	}
	return out
}

func TestBreakingChanges(t *testing.T) {
	cases := []struct {
		name       string
		prev, next string
		want       []string
	}{
		// Removed properties.
		{
			name: "property removed",
			prev: `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			want: []string{"[/properties/b] property_removed: property removed"},
		},
		{
			name: "nested property removed at depth 3",
			prev: `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{
				"c":{"type":"string"},"keep":{"type":"string"}}}}}}}`,
			next: `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{
				"keep":{"type":"string"}}}}}}}`,
			want: []string{"[/properties/a/properties/b/properties/c] property_removed: property removed"},
		},
		{
			name: "property removed inside items",
			prev: `{"type":"object","properties":{"lines":{"type":"array","items":{"type":"object","properties":{
				"sku":{"type":"string"},"qty":{"type":"integer"}}}}}}`,
			next: `{"type":"object","properties":{"lines":{"type":"array","items":{"type":"object","properties":{
				"qty":{"type":"integer"}}}}}}`,
			want: []string{"[/properties/lines/items/properties/sku] property_removed: property removed"},
		},
		{
			name: "items with properties removed",
			prev: `{"type":"object","properties":{"lines":{"type":"array","items":{"type":"object","properties":{"sku":{"type":"string"}}}}}}`,
			next: `{"type":"object","properties":{"lines":{"type":"array"}}}`,
			want: []string{"[/properties/lines/items/properties/sku] property_removed: property removed"},
		},
		{
			name: "additionalProperties with properties made true",
			prev: `{"type":"object","additionalProperties":{"type":"object","properties":{"v":{"type":"string"}}}}`,
			next: `{"type":"object","additionalProperties":true}`,
			want: []string{"[/additionalProperties/properties/v] property_removed: property removed"},
		},
		{
			name: "property name needing pointer escapes",
			prev: `{"type":"object","properties":{"a/b~c":{"type":"string"},"k":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"k":{"type":"string"}}}`,
			want: []string{"[/properties/a~1b~0c] property_removed: property removed"},
		},

		// Types.
		{
			name: "type changed",
			prev: `{"type":"object","properties":{"total":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"total":{"type":"string"}}}`,
			want: []string{
				"[/properties/total] filter_hidden: property no longer accepts range operators",
				"[/properties/total] type_narrowed: type changed from number to string",
			},
		},
		{
			name: "number to integer",
			prev: `{"type":"object","properties":{"total":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"total":{"type":"integer"}}}`,
			want: []string{"[/properties/total] type_narrowed: type changed from number to integer"},
		},
		{
			name: "type added",
			prev: `{"type":"object","properties":{"a":{}}}`,
			next: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			want: []string{"[/properties/a] type_narrowed: type restricted to string"},
		},
		{
			name: "null dropped from type",
			prev: `{"type":"object","properties":{"a":{"type":["string","null"]}}}`,
			next: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			want: []string{"[/properties/a] type_narrowed: type changed from null or string to string"},
		},
		{
			name: "type added that excludes an enum value",
			prev: `{"type":"object","properties":{"o":{"type":"object","properties":{"v":{"enum":["a",1.5]}}}}}`,
			next: `{"type":"object","properties":{"o":{"type":"object","properties":{"v":{"enum":["a",1.5],"type":"string"}}}}}`,
			want: []string{"[/properties/o/properties/v] type_narrowed: type changed from number or string to string"},
		},
		{
			name: "schema becomes false",
			prev: `{"type":"object"}`,
			next: `false`,
			want: []string{"[] type_narrowed: schema no longer accepts any value"},
		},
		{
			name: "property becomes false",
			prev: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"a":false}}`,
			want: []string{
				"[/properties/a] filter_hidden: property no longer filterable: not a scalar type",
				"[/properties/a] type_narrowed: property no longer accepts any value",
			},
		},

		// Enum and const.
		{
			name: "enum value removed",
			prev: `{"type":"object","properties":{"currency":{"type":"string","enum":["USD","EUR"]}}}`,
			next: `{"type":"object","properties":{"currency":{"type":"string","enum":["USD"]}}}`,
			want: []string{`[/properties/currency] enum_narrowed: enum value "EUR" removed`},
		},
		{
			name: "enum added where none",
			prev: `{"type":"object","properties":{"currency":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"currency":{"type":"string","enum":["USD","EUR"]}}}`,
			want: []string{"[/properties/currency] enum_narrowed: enum added"},
		},
		{
			name: "const changed",
			prev: `{"type":"object","properties":{"kind":{"const":"order"}}}`,
			next: `{"type":"object","properties":{"kind":{"const":"refund"}}}`,
			want: []string{`[/properties/kind] const_changed: const changed from "order" to "refund"`},
		},
		{
			name: "const added",
			prev: `{"type":"object","properties":{"kind":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"kind":{"type":"string","const":"order"}}}`,
			want: []string{`[/properties/kind] const_changed: const "order" added`},
		},
		{
			name: "structured enum value removed",
			prev: `{"type":"object","properties":{"v":{"enum":[null,true,{"a":1},[1,2]]}}}`,
			next: `{"type":"object","properties":{"v":{"enum":[null,true,{"a":1.0}]}}}`,
			want: []string{"[/properties/v] enum_narrowed: enum value [1,2] removed"},
		},
		{
			name: "allOf enums intersected",
			prev: `{"type":"object","properties":{"v":{"allOf":[{"enum":["a","b","c"]},{"enum":["b","c","d"]}]}}}`,
			next: `{"type":"object","properties":{"v":{"allOf":[{"enum":["b","x"]},{"enum":["b","y"]}]}}}`,
			want: []string{`[/properties/v/allOf/0] enum_narrowed: enum value "c" removed`},
		},
		{
			name: "enum replaced by a narrower const",
			prev: `{"type":"object","properties":{"kind":{"enum":["order","refund"]}}}`,
			next: `{"type":"object","properties":{"kind":{"const":"order"}}}`,
			want: []string{`[/properties/kind] const_changed: const "order" added`},
		},
		{
			name: "const no longer in enum",
			prev: `{"type":"object","properties":{"kind":{"const":"order"}}}`,
			next: `{"type":"object","properties":{"kind":{"enum":["refund"]}}}`,
			want: []string{`[/properties/kind] enum_narrowed: value "order" no longer allowed`},
		},

		// Numeric and size constraints.
		{
			name: "maximum lowered",
			prev: `{"type":"object","properties":{"total":{"type":"number","maximum":100}}}`,
			next: `{"type":"object","properties":{"total":{"type":"number","maximum":50}}}`,
			want: []string{"[/properties/total] constraint_tightened: maximum lowered from 100 to 50"},
		},
		{
			name: "maximum added",
			prev: `{"type":"object","properties":{"total":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"total":{"type":"number","maximum":50}}}`,
			want: []string{"[/properties/total] constraint_tightened: maximum 50 added"},
		},
		{
			name: "minimum raised",
			prev: `{"type":"object","properties":{"total":{"type":"number","minimum":0}}}`,
			next: `{"type":"object","properties":{"total":{"type":"number","minimum":0.5}}}`,
			want: []string{"[/properties/total] constraint_tightened: minimum raised from 0 to 0.5"},
		},
		{
			name: "minimum made exclusive",
			prev: `{"type":"object","properties":{"total":{"type":"number","minimum":5}}}`,
			next: `{"type":"object","properties":{"total":{"type":"number","exclusiveMinimum":5}}}`,
			want: []string{"[/properties/total] constraint_tightened: minimum 5 tightened to exclusiveMinimum 5"},
		},
		{
			name: "exclusiveMaximum added",
			prev: `{"type":"object","properties":{"total":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"total":{"type":"number","exclusiveMaximum":1e3}}}`,
			want: []string{"[/properties/total] constraint_tightened: exclusiveMaximum 1e3 added"},
		},
		{
			name: "negative maximum lowered",
			prev: `{"type":"object","properties":{"t":{"type":"number","maximum":-1}}}`,
			next: `{"type":"object","properties":{"t":{"type":"number","maximum":-10}}}`,
			want: []string{"[/properties/t] constraint_tightened: maximum lowered from -1 to -10"},
		},
		{
			name: "string length tightened",
			prev: `{"type":"object","properties":{"s":{"type":"string","minLength":1,"maxLength":100}}}`,
			next: `{"type":"object","properties":{"s":{"type":"string","minLength":3,"maxLength":50}}}`,
			want: []string{
				"[/properties/s] constraint_tightened: maxLength lowered from 100 to 50",
				"[/properties/s] constraint_tightened: minLength raised from 1 to 3",
			},
		},
		{
			name: "array and object sizes added",
			prev: `{"type":"object","properties":{"tags":{"type":"array"},"meta":{"type":"object"}}}`,
			next: `{"type":"object","properties":{"tags":{"type":"array","maxItems":10,"minItems":1},
				"meta":{"type":"object","minProperties":1,"maxProperties":5}}}`,
			want: []string{
				"[/properties/meta] constraint_tightened: maxProperties 5 added",
				"[/properties/meta] constraint_tightened: minProperties 1 added",
				"[/properties/tags] constraint_tightened: maxItems 10 added",
				"[/properties/tags] constraint_tightened: minItems 1 added",
			},
		},
		{
			name: "uniqueItems added",
			prev: `{"type":"object","properties":{"tags":{"type":"array"}}}`,
			next: `{"type":"object","properties":{"tags":{"type":"array","uniqueItems":true}}}`,
			want: []string{"[/properties/tags] constraint_tightened: uniqueItems added"},
		},
		{
			name: "multipleOf added",
			prev: `{"type":"object","properties":{"n":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","multipleOf":5}}}`,
			want: []string{"[/properties/n] constraint_tightened: multipleOf 5 added"},
		},
		{
			name: "multipleOf changed to a non-divisor",
			prev: `{"type":"object","properties":{"n":{"type":"number","multipleOf":2}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","multipleOf":3}}}`,
			want: []string{"[/properties/n] constraint_tightened: multipleOf changed from 2 to 3"},
		},
		{
			name: "multipleOf coarsened",
			prev: `{"type":"object","properties":{"n":{"type":"number","multipleOf":0.25}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","multipleOf":0.5}}}`,
			want: []string{"[/properties/n] constraint_tightened: multipleOf changed from 0.25 to 0.5"},
		},

		// String keywords.
		{
			name: "pattern added",
			prev: `{"type":"object","properties":{"sku":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"sku":{"type":"string","pattern":"^[A-Z]+$"}}}`,
			want: []string{`[/properties/sku] constraint_tightened: pattern "^[A-Z]+$" added`},
		},
		{
			name: "pattern changed",
			prev: `{"type":"object","properties":{"sku":{"type":"string","pattern":"^[A-Z0-9]+$"}}}`,
			next: `{"type":"object","properties":{"sku":{"type":"string","pattern":"^[A-Z]+$"}}}`,
			want: []string{`[/properties/sku] constraint_tightened: pattern changed from "^[A-Z0-9]+$" to "^[A-Z]+$"`},
		},
		// Formats are annotations: changing one never fails a payload.
		{
			name: "format added",
			prev: `{"type":"object","properties":{"o":{"type":"object","properties":{"at":{"type":"string"}}}}}`,
			next: `{"type":"object","properties":{"o":{"type":"object","properties":{"at":{"type":"string","format":"date-time"}}}}}`,
		},
		{
			name: "format changed",
			prev: `{"type":"object","properties":{"o":{"type":"object","properties":{"at":{"type":"string","format":"date-time"}}}}}`,
			next: `{"type":"object","properties":{"o":{"type":"object","properties":{"at":{"type":"string","format":"date"}}}}}`,
		},
		{
			name: "items with a format added",
			prev: `{"type":"object","properties":{"days":{"type":"array"}}}`,
			next: `{"type":"object","properties":{"days":{"type":"array","items":{"format":"date"}}}}`,
		},
		{
			// Payloads only widen, but the argument loses its range operators.
			name: "date format replaced by an annotation",
			prev: `{"type":"object","properties":{"d":{"type":"string","format":"date"}}}`,
			next: `{"type":"object","properties":{"d":{"type":"string","format":"email"}}}`,
			want: []string{"[/properties/d] filter_hidden: property no longer accepts range operators"},
		},

		// additionalProperties and items.
		{
			name: "additionalProperties true to false",
			prev: `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":true}`,
			next: `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}`,
			want: []string{"[] constraint_tightened: additional properties no longer allowed"},
		},
		{
			name: "additionalProperties absent to false",
			prev: `{"type":"object","properties":{"meta":{"type":"object"}}}`,
			next: `{"type":"object","properties":{"meta":{"type":"object","additionalProperties":false}}}`,
			want: []string{"[/properties/meta] constraint_tightened: additional properties no longer allowed"},
		},
		{
			name: "additionalProperties schema to false",
			prev: `{"type":"object","additionalProperties":{"type":"string"}}`,
			next: `{"type":"object","additionalProperties":false}`,
			want: []string{"[/additionalProperties] constraint_tightened: additional properties no longer allowed"},
		},
		{
			name: "additionalProperties schema narrowed",
			prev: `{"type":"object","additionalProperties":{"type":["string","number"]}}`,
			next: `{"type":"object","additionalProperties":{"type":"string"}}`,
			want: []string{"[/additionalProperties] type_narrowed: type changed from number or string to string"},
		},
		{
			name: "additionalProperties schema added",
			prev: `{"type":"object"}`,
			next: `{"type":"object","additionalProperties":{"type":"string"}}`,
			want: []string{"[] constraint_tightened: additional properties now constrained"},
		},
		{
			name: "items added",
			prev: `{"type":"object","properties":{"tags":{"type":"array"}}}`,
			next: `{"type":"object","properties":{"tags":{"type":"array","items":{"properties":{"x":{"enum":[1]}}}}}}`,
			want: []string{"[/properties/tags] constraint_tightened: array items now constrained"},
		},
		{
			name: "items added through a $ref",
			prev: `{"type":"object","properties":{"tags":{"type":"array"}},"$defs":{"tag":{"allOf":[{"properties":{"v":{"items":{"uniqueItems":true}}}}]}}}`,
			next: `{"type":"object","properties":{"tags":{"type":"array","items":{"$ref":"#/$defs/tag"}}},"$defs":{"tag":{"allOf":[{"properties":{"v":{"items":{"uniqueItems":true}}}}]}}}`,
			want: []string{"[/properties/tags] constraint_tightened: array items now constrained"},
		},
		{
			name: "items added through an unresolvable $ref",
			prev: `{"type":"object","properties":{"tags":{"type":"array"}}}`,
			next: `{"type":"object","properties":{"tags":{"type":"array","items":{"$ref":"#/$defs/missing"}}}}`,
			want: []string{"[/properties/tags] constraint_tightened: array items now constrained"},
		},
		{
			name: "items become false",
			prev: `{"type":"object","properties":{"tags":{"type":"array","items":{"type":"string"}}}}`,
			next: `{"type":"object","properties":{"tags":{"type":"array","items":false}}}`,
			want: []string{"[/properties/tags/items] constraint_tightened: array items no longer allowed"},
		},
		{
			name: "items type narrowed",
			prev: `{"type":"object","properties":{"tags":{"type":"array","items":{"type":["string","integer"]}}}}`,
			next: `{"type":"object","properties":{"tags":{"type":"array","items":{"type":"string"}}}}`,
			want: []string{"[/properties/tags/items] type_narrowed: type changed from integer or string to string"},
		},

		// $ref.
		{
			name: "change in a $ref'd definition",
			prev: `{"type":"object","properties":{"currency":{"$ref":"#/$defs/currency"}},
				"$defs":{"currency":{"type":"string","enum":["USD","EUR"]}}}`,
			next: `{"type":"object","properties":{"currency":{"$ref":"#/$defs/currency"}},
				"$defs":{"currency":{"type":"string","enum":["USD"]}}}`,
			want: []string{`[/$defs/currency] enum_narrowed: enum value "EUR" removed`},
		},
		{
			name: "shared definition reported once",
			prev: `{"type":"object","properties":{"billing":{"$ref":"#/$defs/address"},"shipping":{"$ref":"#/$defs/address"}},
				"$defs":{"address":{"type":"object","properties":{"city":{"type":"string"},"zip":{"type":"string"}}}}}`,
			next: `{"type":"object","properties":{"billing":{"$ref":"#/$defs/address"},"shipping":{"$ref":"#/$defs/address"}},
				"$defs":{"address":{"type":"object","properties":{"zip":{"type":"string"}}}}}`,
			want: []string{"[/$defs/address/properties/city] property_removed: property removed"},
		},
		{
			name: "inline schema replaced by a narrower $ref",
			prev: `{"type":"object","properties":{"total":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"total":{"$ref":"#/$defs/amount"}},"$defs":{"amount":{"type":"integer"}}}`,
			want: []string{"[/properties/total] type_narrowed: type changed from number to integer"},
		},
		{
			name: "$ref with sibling constraint",
			prev: `{"type":"object","properties":{"total":{"$ref":"#/$defs/amount","maximum":100}},"$defs":{"amount":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"total":{"$ref":"#/$defs/amount","maximum":10}},"$defs":{"amount":{"type":"number"}}}`,
			want: []string{"[/properties/total] constraint_tightened: maximum lowered from 100 to 10"},
		},
		{
			name: "recursive schema",
			prev: `{"type":"object","properties":{"tree":{"$ref":"#/$defs/node"}},"$defs":{"node":{"type":"object","properties":{
				"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"#/$defs/node"}}}}}}`,
			next: `{"type":"object","properties":{"tree":{"$ref":"#/$defs/node"}},"$defs":{"node":{"type":"object","properties":{
				"name":{"type":"integer"},"children":{"type":"array","items":{"$ref":"#/$defs/node"}}}}}}`,
			want: []string{"[/$defs/node/properties/name] type_narrowed: type changed from string to integer"},
		},
		{
			name: "recursive root reference",
			prev: `{"type":"object","properties":{"id":{"type":"string"},"parent":{"$ref":"#"}}}`,
			next: `{"type":"object","properties":{"parent":{"$ref":"#"}}}`,
			want: []string{"[/properties/id] property_removed: property removed"},
		},
		{
			name: "unresolvable reference changed",
			prev: `{"type":"object","properties":{"a":{"$ref":"#/$defs/missing"}}}`,
			next: `{"type":"object","properties":{"a":{"$ref":"#/$defs/other"}}}`,
			want: []string{`[/properties/a] composite_changed: $ref "#/$defs/missing" does not resolve`},
		},
		{
			// What a reference that resolves on neither side pointed at can't
			// be compared, so it never counts as unchanged.
			name: "unresolvable reference unchanged",
			prev: `{"type":"object","properties":{"a":{"$ref":"#/$defs/missing"},"b":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"a":{"$ref":"#/$defs/missing","description":"A."},"b":{"type":"string","title":"B"}}}`,
			want: []string{`[/properties/a] composite_changed: $ref "#/$defs/missing" does not resolve`},
		},
		{
			// The catalog rejects anchors; a snapshot written before it did
			// can't hide a narrowed target behind one.
			name: "anchor reference target narrowed",
			prev: `{"type":"object","properties":{"price":{"$ref":"#money"}},"$defs":{"m":{"$anchor":"money","type":"number"}}}`,
			next: `{"type":"object","properties":{"price":{"$ref":"#money"}},"$defs":{"m":{"$anchor":"money","type":"string"}}}`,
			want: []string{`[/properties/price] composite_changed: $ref "#money" does not resolve`},
		},
		{
			name: "$dynamicRef target narrowed",
			prev: `{"type":"object","properties":{"price":{"$dynamicRef":"#/$defs/m"}},"$defs":{"m":{"type":"number"}}}`,
			next: `{"type":"object","properties":{"price":{"$dynamicRef":"#/$defs/m"}},"$defs":{"m":{"type":"string"}}}`,
			want: []string{"[/properties/price] composite_changed: $dynamicRef changed"},
		},

		// allOf.
		{
			name: "allOf branch loses a property",
			prev: `{"type":"object","allOf":[{"$ref":"#/$defs/base"},{"properties":{"total":{"type":"number"}}}],
				"$defs":{"base":{"type":"object","properties":{"id":{"type":"string"},"at":{"type":"string"}}}}}`,
			next: `{"type":"object","allOf":[{"$ref":"#/$defs/base"},{"properties":{"total":{"type":"number"}}}],
				"$defs":{"base":{"type":"object","properties":{"at":{"type":"string"}}}}}`,
			want: []string{"[/$defs/base/properties/id] property_removed: property removed"},
		},
		{
			name: "allOf property moved out and narrowed",
			prev: `{"type":"object","allOf":[{"properties":{"total":{"type":"number"}}},{"properties":{"note":{"type":"string"}}}]}`,
			next: `{"type":"object","properties":{"total":{"type":"integer"},"note":{"type":"string"}}}`,
			want: []string{"[/allOf/0/properties/total] type_narrowed: type changed from number to integer"},
		},
		{
			name: "allOf branches tighten the same property",
			prev: `{"type":"object","allOf":[{"properties":{"n":{"type":"number","maximum":100}}},{"properties":{"n":{"minimum":0}}}]}`,
			next: `{"type":"object","allOf":[{"properties":{"n":{"type":"number","maximum":100}}},{"properties":{"n":{"minimum":1}}}]}`,
			want: []string{"[/allOf/1/properties/n] constraint_tightened: minimum raised from 0 to 1"},
		},
		{
			name: "wide allOf",
			prev: wideAllOf(40, -1),
			next: wideAllOf(40, 17),
			want: []string{"[/allOf/17/properties/p17] property_removed: property removed"},
		},
		{
			name: "self-referencing allOf",
			prev: `{"$ref":"#/$defs/a","$defs":{"a":{"allOf":[{"$ref":"#/$defs/a"}],"properties":{"x":{"type":"number"}}}}}`,
			next: `{"$ref":"#/$defs/a","$defs":{"a":{"allOf":[{"$ref":"#/$defs/a"}],"properties":{"x":{"type":"integer"}}}}}`,
			want: []string{"[/$defs/a/properties/x] type_narrowed: type changed from number to integer"},
		},
		{
			name: "allOf with an unresolvable branch is compared by equality",
			prev: `{"type":"object","properties":{"a":{"allOf":[{"$ref":"#/$defs/x"},{"type":"string"}]}}}`,
			next: `{"type":"object","properties":{"a":{"allOf":[{"$ref":"#/$defs/x"},{"type":"string","maxLength":3}]}}}`,
			want: []string{"[/properties/a] composite_changed: allOf changed"},
		},

		// Other composites.
		{
			name: "anyOf changed",
			prev: `{"type":"object","properties":{"v":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}`,
			next: `{"type":"object","properties":{"v":{"anyOf":[{"type":"string"}]}}}`,
			want: []string{"[/properties/v] composite_changed: anyOf changed"},
		},
		{
			name: "anyOf added",
			prev: `{"type":"object","properties":{"v":{}}}`,
			next: `{"type":"object","properties":{"v":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}`,
			want: []string{"[/properties/v] composite_changed: anyOf added"},
		},
		{
			name: "oneOf removed",
			prev: `{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}`,
			next: `{"type":"object","properties":{"v":{}}}`,
			want: []string{"[/properties/v] composite_changed: oneOf removed"},
		},
		{
			name: "anyOf branch $ref target changed",
			prev: `{"type":"object","properties":{"ship":{"anyOf":[{"$ref":"#/$defs/addr"},{"type":"null"}]}},
				"$defs":{"addr":{"type":"object","properties":{"zip":{"$ref":"#/$defs/zip"}}},"zip":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"ship":{"anyOf":[{"$ref":"#/$defs/addr"},{"type":"null"}]}},
				"$defs":{"addr":{"type":"object","properties":{"zip":{"$ref":"#/$defs/zip"}}},"zip":{"type":"integer"}}}`,
			want: []string{"[/properties/ship] composite_changed: anyOf changed"},
		},
		{
			name: "patternProperties changed",
			prev: `{"type":"object","patternProperties":{"^x-":{"type":"string"}}}`,
			next: `{"type":"object","patternProperties":{"^x-":{"type":"integer"}}}`,
			want: []string{"[] composite_changed: patternProperties changed"},
		},
		{
			name: "if then added",
			prev: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"a":{"type":"string"}},"if":{"required":["a"]},"then":{"required":["b"]}}`,
			want: []string{"[] composite_changed: if added", "[] composite_changed: then added"},
		},

		// Filterability of top-level properties.
		{
			name: "x-mcp-filter false newly set",
			prev: `{"type":"object","properties":{"status":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"status":{"type":"string","x-mcp-filter":false}}}`,
			want: []string{"[/properties/status] filter_hidden: property no longer filterable: x-mcp-filter is false"},
		},
		{
			name: "x-mcp-filter false set on the $ref target",
			prev: `{"type":"object","properties":{"status":{"$ref":"#/$defs/s"}},"$defs":{"s":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"status":{"$ref":"#/$defs/s"}},"$defs":{"s":{"type":"string","x-mcp-filter":false}}}`,
			want: []string{"[/properties/status] filter_hidden: property no longer filterable: x-mcp-filter is false"},
		},
		{
			name: "property becomes an object",
			prev: `{"type":"object","properties":{"customer":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"customer":{"type":"object","properties":{"id":{"type":"string"}}}}}`,
			want: []string{
				"[/properties/customer] filter_hidden: property no longer filterable: not a scalar type",
				"[/properties/customer] type_narrowed: type changed from string to object",
			},
		},
		{
			name: "filterable property moved into allOf",
			prev: `{"type":"object","properties":{"status":{"type":"string"}}}`,
			next: `{"type":"object","allOf":[{"properties":{"status":{"type":"string"}}}]}`,
			want: []string{"[/properties/status] filter_hidden: property no longer filterable: not declared in the root properties"},
		},
		{
			// Widening on its own, but the argument leaves the inputSchema.
			name: "top-level type removed",
			prev: `{"type":"object","properties":{"total":{"type":"integer"}}}`,
			next: `{"type":"object","properties":{"total":{}}}`,
			want: []string{"[/properties/total] filter_hidden: property no longer filterable: not a scalar type"},
		},
		{
			name: "date format removed from a filterable property",
			prev: `{"type":"object","properties":{"d":{"type":"string","format":"date"}}}`,
			next: `{"type":"object","properties":{"d":{"type":"string"}}}`,
			want: []string{"[/properties/d] filter_hidden: property no longer accepts range operators"},
		},
		{
			name: "dated property widened to another type",
			prev: `{"type":"object","properties":{"d":{"type":"string","format":"date"}}}`,
			next: `{"type":"object","properties":{"d":{"type":["string","boolean"],"format":"date"}}}`,
			want: []string{"[/properties/d] filter_hidden: property no longer accepts range operators"},
		},
		{
			name: "dated property through a $ref loses its format",
			prev: `{"type":"object","properties":{"d":{"$ref":"#/$defs/day"}},"$defs":{"day":{"type":"string","format":"date"}}}`,
			next: `{"type":"object","properties":{"d":{"$ref":"#/$defs/day"}},"$defs":{"day":{"type":"string"}}}`,
			want: []string{"[/properties/d] filter_hidden: property no longer accepts range operators"},
		},
		{
			name: "date to date-time",
			prev: `{"type":"object","properties":{"d":{"type":"string","format":"date"}}}`,
			next: `{"type":"object","properties":{"d":{"type":"string","format":"date-time"}}}`,
			want: []string{"[/properties/d] filter_hidden: property no longer accepts range operators"},
		},
		{
			name: "removed filterable property is only reported as removed",
			prev: `{"type":"object","properties":{"status":{"type":"string"},"id":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"id":{"type":"string"}}}`,
			want: []string{"[/properties/status] property_removed: property removed"},
		},

		// Non-breaking changes.
		{name: "integer to number", prev: `{"type":"object","properties":{"total":{"type":"integer"}}}`,
			next: `{"type":"object","properties":{"total":{"type":"number"}}}`},
		{name: "type removed", prev: `{"type":"object","properties":{"o":{"type":"object","properties":{"n":{"type":"integer"}}}}}`,
			next: `{"type":"object","properties":{"o":{"type":"object","properties":{"n":{}}}}}`},
		{name: "type added that the const implies", prev: `{"type":"object","properties":{"k":{"const":"order"}}}`,
			next: `{"type":"object","properties":{"k":{"type":"string","const":"order"}}}`},
		{name: "integer type added to integral enum", prev: `{"type":"object","properties":{"n":{"enum":[1,2.0]}}}`,
			next: `{"type":"object","properties":{"n":{"type":"integer","enum":[1,2]}}}`},
		{name: "null added to type", prev: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"a":{"type":["string","null"]}}}`},
		{name: "new optional property", prev: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"object"}}}`},
		{name: "new required property", prev: `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
			next: `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["a","b"]}`},
		{name: "required removed", prev: `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
			next: `{"type":"object","properties":{"a":{"type":"string"}}}`},
		{name: "enum value added", prev: `{"type":"object","properties":{"c":{"type":"string","enum":["USD"]}}}`,
			next: `{"type":"object","properties":{"c":{"type":"string","enum":["USD","EUR"]}}}`},
		{name: "enum removed", prev: `{"type":"object","properties":{"c":{"type":"string","enum":["USD"]}}}`,
			next: `{"type":"object","properties":{"c":{"type":"string"}}}`},
		{name: "enum numbers compared by value", prev: `{"type":"object","properties":{"n":{"enum":[1,2.50]}}}`,
			next: `{"type":"object","properties":{"n":{"enum":[1.0,2.5,1e1]}}}`},
		{name: "const removed", prev: `{"type":"object","properties":{"k":{"const":"order"}}}`,
			next: `{"type":"object","properties":{"k":{"type":"string"}}}`},
		{name: "const widened to enum", prev: `{"type":"object","properties":{"k":{"const":"order"}}}`,
			next: `{"type":"object","properties":{"k":{"enum":["order","refund"]}}}`},
		{name: "enum narrowed to what const allowed", prev: `{"type":"object","properties":{"k":{"enum":["a","b"],"const":"a"}}}`,
			next: `{"type":"object","properties":{"k":{"enum":["a"]}}}`},
		{name: "allOf enums widened", prev: `{"type":"object","properties":{"v":{"allOf":[{"enum":["a","b","c"]},{"enum":["b","c","d"]}]}}}`,
			next: `{"type":"object","properties":{"v":{"allOf":[{"enum":["b","c","x"]},{"enum":["b","c","y"]}]}}}`},
		{name: "type added that structured enum values imply",
			prev: `{"type":"object","properties":{"v":{"enum":[null,true,{"a":1},[1]]}}}`,
			next: `{"type":"object","properties":{"v":{"enum":[null,true,{"a":1},[1]],"type":["null","boolean","object","array"]}}}`},
		{name: "items added through a $ref to annotations",
			prev: `{"type":"object","properties":{"t":{"type":"array"}},"$defs":{"any":{"allOf":[{"description":"Anything."}]}}}`,
			next: `{"type":"object","properties":{"t":{"type":"array","items":{"$ref":"#/$defs/any"}}},"$defs":{"any":{"allOf":[{"description":"Anything."}]}}}`},
		{name: "maximum raised", prev: `{"type":"object","properties":{"n":{"type":"number","maximum":50}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","maximum":100}}}`},
		{name: "minimum lowered", prev: `{"type":"object","properties":{"n":{"type":"number","minimum":1}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","minimum":-1}}}`},
		{name: "exclusive bound made inclusive", prev: `{"type":"object","properties":{"n":{"type":"number","exclusiveMinimum":5}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","minimum":5}}}`},
		{name: "equal bound rewritten", prev: `{"type":"object","properties":{"n":{"type":"number","maximum":100}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","maximum":1e2}}}`},
		{name: "length limits relaxed", prev: `{"type":"object","properties":{"s":{"type":"string","minLength":2,"maxLength":5}}}`,
			next: `{"type":"object","properties":{"s":{"type":"string","maxLength":10}}}`},
		{name: "pattern and format removed", prev: `{"type":"object","properties":{"s":{"type":"string","pattern":"^a","format":"email"}}}`,
			next: `{"type":"object","properties":{"s":{"type":"string"}}}`},
		{name: "annotation format added", prev: `{"type":"object","properties":{"email":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"email":{"type":"string","format":"email"}}}`},
		{name: "annotation format changed", prev: `{"type":"object","properties":{"id":{"type":"string","format":"uuid"}}}`,
			next: `{"type":"object","properties":{"id":{"type":"string","format":"uri"}}}`},
		{name: "date-time format replaced by an annotation", prev: `{"type":"object","properties":{"at":{"type":"string","format":"date-time"}}}`,
			next: `{"type":"object","properties":{"at":{"type":"string","format":"email"}}}`},
		{name: "items with an annotation format added", prev: `{"type":"object","properties":{"t":{"type":"array"}}}`,
			next: `{"type":"object","properties":{"t":{"type":"array","items":{"format":"email"}}}}`},
		{name: "dated property widened to an integer keeps range operators", prev: `{"type":"object","properties":{"d":{"type":["string","null"],"format":"date"}}}`,
			next: `{"type":"object","properties":{"d":{"type":["string","null","integer"],"format":"date"}}}`},
		{name: "integer widened to a string keeps range operators", prev: `{"type":"object","properties":{"n":{"type":"integer"}}}`,
			next: `{"type":"object","properties":{"n":{"type":["integer","string"]}}}`},
		{name: "multipleOf refined", prev: `{"type":"object","properties":{"n":{"type":"number","multipleOf":4}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","multipleOf":2}}}`},
		{name: "multipleOf refined to a fraction", prev: `{"type":"object","properties":{"n":{"type":"number","multipleOf":0.5}}}`,
			next: `{"type":"object","properties":{"n":{"type":"number","multipleOf":0.25}}}`},
		{name: "uniqueItems removed", prev: `{"type":"object","properties":{"t":{"type":"array","uniqueItems":true}}}`,
			next: `{"type":"object","properties":{"t":{"type":"array"}}}`},
		{name: "additionalProperties false to true", prev: `{"type":"object","additionalProperties":false}`,
			next: `{"type":"object","additionalProperties":true}`},
		{name: "additionalProperties false to schema", prev: `{"type":"object","additionalProperties":false}`,
			next: `{"type":"object","additionalProperties":{"type":"string"}}`},
		{name: "items removed", prev: `{"type":"object","properties":{"t":{"type":"array","items":{"type":"string"}}}}`,
			next: `{"type":"object","properties":{"t":{"type":"array"}}}`},
		{name: "items with only annotations added", prev: `{"type":"object","properties":{"t":{"type":"array"}}}`,
			next: `{"type":"object","properties":{"t":{"type":"array","items":{"description":"A tag."}}}}`},
		{name: "x-mcp-filter false removed", prev: `{"type":"object","properties":{"id":{"type":"string","x-mcp-filter":false}}}`,
			next: `{"type":"object","properties":{"id":{"type":"string"}}}`},
		{name: "forbidden property removed", prev: `{"type":"object","properties":{"a":{"type":"string"},"legacy":false}}`,
			next: `{"type":"object","properties":{"a":{"type":"string"}}}`},
		{
			name: "description-only change",
			prev: `{"type":"object","description":"An order.","properties":{"total":{"type":"number","description":"Total."}}}`,
			next: `{"type":"object","description":"A placed order.","title":"Order","properties":{"total":{"type":"number",
				"description":"Total in major units.","examples":[1.5],"default":0,"$comment":"cents before v2"}}}`,
		},
		{
			name: "key-order-only change",
			prev: `{"type":"object","properties":{"a":{"type":"string","enum":["x","y"]},"b":{"maximum":3,"type":"integer"}},"required":["a"]}`,
			next: `{"required":["a"],"properties":{"b":{"type":"integer","maximum":3},"a":{"enum":["x","y"],"type":"string"}},"type":"object"}`,
		},
		{
			name: "property inlined from a $ref",
			prev: `{"type":"object","properties":{"c":{"$ref":"#/$defs/c"}},"$defs":{"c":{"type":"string","enum":["USD"]}}}`,
			next: `{"type":"object","properties":{"c":{"type":"string","enum":["USD"]}}}`,
		},
		{
			name: "definition renamed",
			prev: `{"type":"object","properties":{"c":{"$ref":"#/$defs/c"}},"$defs":{"c":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"c":{"$ref":"#/$defs/currency"}},"$defs":{"currency":{"type":"string"}}}`,
		},
		{
			name: "allOf branch gains an optional property",
			prev: `{"type":"object","allOf":[{"$ref":"#/$defs/base"},{"type":"object","properties":{"total":{"type":"number"}}}],
				"$defs":{"base":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}}`,
			next: `{"type":"object","allOf":[{"$ref":"#/$defs/base"},{"type":"object","properties":{"total":{"type":"number"}}}],
				"$defs":{"base":{"type":"object","properties":{"id":{"type":"string"},"at":{"type":"string"}},"required":["id"]}}}`,
		},
		{
			name: "allOf branches reordered",
			prev: `{"type":"object","allOf":[{"properties":{"a":{"type":"string"}}},{"properties":{"b":{"type":"string"}}}]}`,
			next: `{"type":"object","allOf":[{"properties":{"b":{"type":"string"}}},{"properties":{"a":{"type":"string"}}}]}`,
		},
		{
			name: "allOf wrapper around a $ref widened",
			prev: `{"type":"object","properties":{"n":{"allOf":[{"$ref":"#/$defs/n"}],"description":"N."}},"$defs":{"n":{"type":"integer"}}}`,
			next: `{"type":"object","properties":{"n":{"allOf":[{"$ref":"#/$defs/n"}],"description":"N!"}},"$defs":{"n":{"type":"number"}}}`,
		},
		{
			name: "anyOf with annotation changes",
			prev: `{"type":"object","properties":{"v":{"anyOf":[{"type":"string","description":"a"},{"type":"null"}]}}}`,
			next: `{"type":"object","properties":{"v":{"anyOf":[{"description":"b","type":"string","x-note":1},{"type":"null"}]}}}`,
		},
		{
			name: "anyOf with an unrelated definition changed",
			prev: `{"type":"object","properties":{"v":{"anyOf":[{"$ref":"#/$defs/a"},{"type":"null"}]}},"$defs":{"a":{"type":"string"},"b":{"type":"string"}}}`,
			next: `{"type":"object","properties":{"v":{"anyOf":[{"$ref":"#/$defs/a"},{"type":"null"}]}},"$defs":{"a":{"type":"string"},"b":{"type":"integer"}}}`,
		},
		{
			name: "prev schema false",
			prev: `false`,
			next: `{"type":"object","properties":{"a":{"type":"string"}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, breaking(tc.prev, tc.next))
		})
	}
}

// wideAllOf returns a schema with n allOf branches declaring one property
// each, without branch skip's property.
func wideAllOf(n, skip int) string {
	branches := make([]string, n)
	for i := range n {
		if i == skip {
			branches[i] = `{"type":"object"}`
		} else {
			branches[i] = fmt.Sprintf(`{"type":"object","properties":{"p%d":{"type":"string"}}}`, i)
		}
	}
	return `{"type":"object","allOf":[` + strings.Join(branches, ",") + `]}`
}

func TestBreakingChangesFixtures(t *testing.T) {
	load := func(name string) string {
		data, err := os.ReadFile(filepath.Join("testdata", "evolution", name))
		require.NoError(t, err)
		return string(data)
	}
	v1, v2, v3 := load("order.v1.json"), load("order.v2.json"), load("order.v3.json")

	t.Run("additive evolution", func(t *testing.T) {
		assert.Empty(t, breaking(v1, v2))
	})
	t.Run("breaking evolution", func(t *testing.T) {
		assert.Equal(t, []string{
			"[] constraint_tightened: additional properties no longer allowed",
			`[/$defs/Currency] enum_narrowed: enum value "EUR" removed`,
			"[/$defs/Line/properties/quantity] constraint_tightened: maximum 1000 added",
			`[/$defs/Line/properties/sku] constraint_tightened: pattern changed from "^[A-Z0-9-]+$" to "^[A-Z]+$"`,
			"[/$defs/Party/properties/name] property_removed: property removed",
			// Address only relaxed, but anyOf is compared by equality.
			"[/properties/shipping] composite_changed: anyOf changed",
			"[/properties/status] filter_hidden: property no longer filterable: x-mcp-filter is false",
			"[/properties/total] filter_hidden: property no longer accepts range operators",
			"[/properties/total] type_narrowed: type changed from number to string",
		}, breaking(v2, v3))
	})
	t.Run("rollback narrows", func(t *testing.T) {
		assert.Equal(t, []string{
			`[/$defs/Currency] enum_narrowed: enum value "GBP" removed`,
			"[/$defs/Customer/allOf/0/properties/loyalty_id] property_removed: property removed",
			"[/$defs/Line/properties/discount] property_removed: property removed",
			"[/$defs/Line/properties/quantity] constraint_tightened: minimum raised from 0 to 1",
			"[/$defs/Party/properties/phone] property_removed: property removed",
			"[/properties/lines] constraint_tightened: maxItems lowered from 500 to 100",
			"[/properties/note] property_removed: property removed",
			`[/properties/status] enum_narrowed: enum value "cancelled" removed`,
			"[/properties/total] type_narrowed: type changed from number to integer",
		}, breaking(v2, v1))
	})
}

func TestBreakingChangesTopics(t *testing.T) {
	prev := Snapshot{Topics: map[string]SnapshotTopic{
		"a": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)},
		"b": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{"y":{"type":"string"}}}`)},
		"c": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{"z":{"type":"string"}}}`)},
		"d": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{"w":{"type":"string"}}}`)},
		"e": {},
	}}
	next := Snapshot{Topics: map[string]SnapshotTopic{
		"a": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object"}`)},
		"b": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object"}`)},
		"c": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object"}`)},
		// d is gone; that's an ended topic, not a breaking change.
		"e": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object"}`)},
	}}

	changes := BreakingChanges(prev, next, []string{"c", "a", "d", "e", "a", "missing"})
	assert.Equal(t, []Change{
		{Topic: "a", Path: "/properties/x", Kind: ChangePropertyRemoved, Detail: "property removed"},
		{Topic: "c", Path: "/properties/z", Kind: ChangePropertyRemoved, Detail: "property removed"},
	}, changes)

	assert.Nil(t, BreakingChanges(prev, next, nil))
	assert.Nil(t, BreakingChanges(prev, prev, []string{"a", "b", "c", "d"}))
	assert.Nil(t, BreakingChanges(Snapshot{}, next, []string{"a"}))
}

func TestBreakingChangesDeterministic(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "evolution", "order.v2.json"))
	require.NoError(t, err)
	v2 := json.RawMessage(data)
	data, err = os.ReadFile(filepath.Join("testdata", "evolution", "order.v3.json"))
	require.NoError(t, err)
	v3 := json.RawMessage(data)

	prev := Snapshot{Topics: map[string]SnapshotTopic{}}
	next := Snapshot{Topics: map[string]SnapshotTopic{}}
	var topics []string
	for i := range 5 {
		name := fmt.Sprintf("topic.%d", i)
		prev.Topics[name] = SnapshotTopic{MCPEnabled: true, PayloadSchema: v2}
		next.Topics[name] = SnapshotTopic{MCPEnabled: true, PayloadSchema: v3}
		topics = append([]string{name}, topics...)
	}

	first := BreakingChanges(prev, next, topics)
	require.Len(t, first, 45)
	for range 20 {
		require.Equal(t, first, BreakingChanges(prev, next, topics))
	}
	assert.IsNonDecreasing(t, renderTopicPaths(first))
}

func renderTopicPaths(changes []Change) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Topic + "\x00" + c.Path + "\x00" + c.Kind + "\x00" + c.Detail
	}
	return out
}

func TestBreakingChangesInvalidJSON(t *testing.T) {
	valid := `{"type":"object"}`
	assert.Equal(t, []string{"[] composite_changed: previous schema is not valid JSON"}, breaking(`{"type":`, valid))
	assert.Equal(t, []string{"[] composite_changed: schema is not valid JSON"}, breaking(valid, `{"type":`))
}

// dagSchema returns a schema whose $defs form a DAG of the given depth: each
// level has two properties referencing the next level, so expanding it as a
// tree would visit 2^depth nodes.
func dagSchema(depth int, leaf string) string {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{"root":{"$ref":"#/$defs/d0"}},"$defs":{`)
	for i := range depth {
		fmt.Fprintf(&b, `"d%d":{"type":"object","properties":{"a":{"$ref":"#/$defs/d%d"},"b":{"$ref":"#/$defs/d%d"}}},`, i, i+1, i+1)
	}
	fmt.Fprintf(&b, `"d%d":{"type":"object","properties":{"leaf":%s}}}}`, depth, leaf)
	return b.String()
}

func TestBreakingChangesRefDAG(t *testing.T) {
	prev := dagSchema(60, `{"type":"number"}`)
	next := dagSchema(60, `{"type":"integer"}`)

	start := time.Now()
	got := breaking(prev, next)
	elapsed := time.Since(start)

	assert.Equal(t, []string{"[/$defs/d60/properties/leaf] type_narrowed: type changed from number to integer"}, got)
	assert.Less(t, elapsed, time.Second)
}

func TestBreakingChangesTooComplex(t *testing.T) {
	// prev shares one definition per level; next spells every path out, so
	// each next node pairs with prev's definition: about 111k distinct pairs.
	var prev strings.Builder
	prev.WriteString(`{"type":"object","properties":{"root":{"$ref":"#/$defs/l0"}},"$defs":{`)
	for level := range 5 {
		fmt.Fprintf(&prev, `"l%d":{"type":"object","properties":{`, level)
		for p := range 10 {
			if p > 0 {
				prev.WriteByte(',')
			}
			fmt.Fprintf(&prev, `"p%d":{"$ref":"#/$defs/l%d"}`, p, level+1)
		}
		prev.WriteString(`}},`)
	}
	prev.WriteString(`"l5":{"type":"object"}}}`)

	var tree func(b *strings.Builder, depth int)
	tree = func(b *strings.Builder, depth int) {
		b.WriteString(`{"type":"object"`)
		if depth > 0 {
			b.WriteString(`,"properties":{`)
			for p := range 10 {
				if p > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(b, `"p%d":`, p)
				tree(b, depth-1)
			}
			b.WriteByte('}')
		}
		b.WriteByte('}')
	}
	var next strings.Builder
	next.WriteString(`{"type":"object","properties":{"root":`)
	tree(&next, 5)
	next.WriteString(`}}`)

	start := time.Now()
	got := breaking(prev.String(), next.String())
	elapsed := time.Since(start)

	assert.Equal(t, []string{"[] composite_changed: schema too complex to compare"}, got)
	assert.Less(t, elapsed, 5*time.Second)
}

func TestDiffSchemasBudget(t *testing.T) {
	prev := json.RawMessage(dagSchema(3, `{"type":"number"}`))
	next := json.RawMessage(dagSchema(3, `{"type":"integer"}`))

	got := diffSchemas("t", prev, next, 10)
	require.NotEmpty(t, got)
	assert.Equal(t, Change{Topic: "t", Kind: ChangeCompositeChanged, Detail: "schema too complex to compare"}, got[len(got)-1])

	got = diffSchemas("t", prev, next, maxDiffComparisons)
	assert.Equal(t, []Change{{Topic: "t", Path: "/$defs/d3/properties/leaf", Kind: ChangeTypeNarrowed,
		Detail: "type changed from number to integer"}}, got)
}

func TestBreakingChangesLimit(t *testing.T) {
	var props []string
	for i := range maxChangesPerTopic + 50 {
		props = append(props, fmt.Sprintf(`"p%03d":{"type":"object"}`, i))
	}
	prev := `{"type":"object","properties":{` + strings.Join(props, ",") + `}}`

	got := BreakingChanges(schemaSnapshot(prev), schemaSnapshot(`{"type":"object"}`), []string{evolutionTopic})
	require.Len(t, got, maxChangesPerTopic+1)
	assert.Equal(t, Change{Topic: evolutionTopic, Kind: ChangeCompositeChanged,
		Detail: "more breaking changes not listed (limit 100)"}, got[0])
	assert.Equal(t, "/properties/p000", got[1].Path)
	assert.Equal(t, "/properties/p099", got[maxChangesPerTopic].Path)
}

func TestBreakingChangesLongValuesTruncated(t *testing.T) {
	long := strings.Repeat("é", 100)
	got := breaking(
		`{"type":"object","properties":{"s":{"type":"string"}}}`,
		`{"type":"object","properties":{"s":{"type":"string","pattern":"`+long+`"}}}`,
	)
	require.Len(t, got, 1)
	detail := got[0][strings.Index(got[0], "pattern"):]
	assert.True(t, strings.HasSuffix(detail, "… added"), detail)
	assert.Less(t, len(detail), 100)
}

func TestEndedTopics(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	prev := Snapshot{Topics: map[string]SnapshotTopic{
		"disabled":       {MCPEnabled: true, PayloadSchema: schema},
		"removed":        {MCPEnabled: true, PayloadSchema: schema},
		"schema.removed": {MCPEnabled: true, PayloadSchema: schema},
		"schema.null":    {MCPEnabled: true, PayloadSchema: schema},
		"kept":           {MCPEnabled: true, PayloadSchema: schema},
		"never":          {PayloadSchema: schema},
	}}
	next := Snapshot{Topics: map[string]SnapshotTopic{
		"disabled":       {PayloadSchema: schema},
		"schema.removed": {MCPEnabled: true},
		"schema.null":    {MCPEnabled: true, PayloadSchema: json.RawMessage("null")},
		"kept":           {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
		"never":          {MCPEnabled: true, PayloadSchema: schema},
		"added":          {MCPEnabled: true, PayloadSchema: schema},
	}}

	assert.Equal(t, []string{"disabled", "removed", "schema.null", "schema.removed"}, EndedTopics(prev, next))
	assert.Empty(t, EndedTopics(next, next))
	assert.Empty(t, EndedTopics(Snapshot{}, next))
	assert.Equal(t, []string{"disabled", "kept", "removed", "schema.null", "schema.removed"}, EndedTopics(prev, Snapshot{}))
}

func TestSnapshotHash(t *testing.T) {
	base := Snapshot{Topics: map[string]SnapshotTopic{
		"order.created": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string","enum":["x","y"]}}}`)},
		"order.updated": {},
	}}
	reordered := Snapshot{Topics: map[string]SnapshotTopic{
		"order.updated": {},
		"order.created": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{
			"properties": {"a": {"enum": ["x", "y"], "type": "string"}},
			"type": "object"
		}`)},
	}}
	h := base.Hash()
	assert.Len(t, h, 64)
	assert.Equal(t, h, reordered.Hash())
	assert.Equal(t, h, base.Hash())

	// Round-tripping through JSON, as the persisted baseline does, keeps it.
	data, err := json.Marshal(base)
	require.NoError(t, err)
	var decoded Snapshot
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, h, decoded.Hash())

	changed := map[string]Snapshot{
		"mcp flag": {Topics: map[string]SnapshotTopic{
			"order.created": base.Topics["order.created"],
			"order.updated": {MCPEnabled: true},
		}},
		"enum order": {Topics: map[string]SnapshotTopic{
			"order.created": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string","enum":["y","x"]}}}`)},
			"order.updated": {},
		}},
		"description": {Topics: map[string]SnapshotTopic{
			"order.created": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","description":"d","properties":{"a":{"type":"string","enum":["x","y"]}}}`)},
			"order.updated": {},
		}},
		"topic added": {Topics: map[string]SnapshotTopic{
			"order.created": base.Topics["order.created"],
			"order.updated": {},
			"order.deleted": {},
		}},
		"topic removed": {Topics: map[string]SnapshotTopic{
			"order.created": base.Topics["order.created"],
		}},
		"invalid schema": {Topics: map[string]SnapshotTopic{
			"order.created": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":`)},
			"order.updated": {},
		}},
	}
	for name, s := range changed {
		assert.NotEqual(t, h, s.Hash(), name)
	}

	assert.Equal(t, Snapshot{}.Hash(), Snapshot{Topics: map[string]SnapshotTopic{}}.Hash())
	assert.Equal(t, sha256Hex([]byte(`{"topics":{}}`)), Snapshot{}.Hash())
}

func TestSnapshotTopicHash(t *testing.T) {
	s := Snapshot{Topics: map[string]SnapshotTopic{
		"a": {MCPEnabled: true, PayloadSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)},
		"b": {MCPEnabled: true, PayloadSchema: json.RawMessage(` {"properties":{"x":{"type":"string"}},"type":"object"} `)},
		"c": {PayloadSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)},
		"d": {},
		"e": {PayloadSchema: json.RawMessage(`null`)},
	}}

	assert.Equal(t, "", s.TopicHash("missing"))
	assert.Len(t, s.TopicHash("a"), 64)
	assert.Equal(t, s.TopicHash("a"), s.TopicHash("b"), "key order and whitespace don't matter")
	assert.NotEqual(t, s.TopicHash("a"), s.TopicHash("c"), "the MCP flag matters")
	assert.Equal(t, sha256Hex([]byte(`{"mcp_enabled":false}`)), s.TopicHash("d"))
	assert.Equal(t, s.TopicHash("d"), s.TopicHash("e"))
	assert.Equal(t,
		sha256Hex([]byte(`{"mcp_enabled":true,"payload_schema":{"properties":{"x":{"type":"string"}},"type":"object"}}`)),
		s.TopicHash("a"))
}

func TestJSONDecimal(t *testing.T) {
	cases := []struct {
		a, b string
		cmp  int
	}{
		{"1", "1.0", 0},
		{"100", "1e2", 0},
		{"0.5", "5E-1", 0},
		{"-0", "0", 0},
		{"0.00", "0e10", 0},
		{"5", "10", -1},
		{"0.123", "0.13", -1},
		{"-5", "-10", 1},
		{"-1", "0", -1},
		{"1e400", "1e399", 1},
		{"1e99999999999999999999", "1e400", 1},
		{"-1e-99999999999999999999", "0", -1},
	}
	for _, tc := range cases {
		a, ok := parseJSONDecimal(tc.a)
		require.True(t, ok, tc.a)
		b, ok := parseJSONDecimal(tc.b)
		require.True(t, ok, tc.b)
		assert.Equal(t, tc.cmp, a.cmp(b), "%s vs %s", tc.a, tc.b)
		assert.Equal(t, -tc.cmp, b.cmp(a), "%s vs %s", tc.b, tc.a)
		if tc.cmp == 0 {
			assert.Equal(t, a.key(), b.key(), "%s vs %s", tc.a, tc.b)
		}
	}

	multiples := []struct {
		a, b string
		ok   bool
	}{
		{"4", "2", true},
		{"2", "4", false},
		{"3", "2", false},
		{"0.5", "0.25", true},
		{"0.25", "0.5", false},
		{"1e3", "0.001", true},
		{"1e2000", "1", false}, // beyond the computed range: reported, not computed
	}
	for _, tc := range multiples {
		a, _ := parseJSONDecimal(tc.a)
		b, _ := parseJSONDecimal(tc.b)
		assert.Equal(t, tc.ok, isMultipleOf(a, b), "%s multiple of %s", tc.a, tc.b)
	}
}

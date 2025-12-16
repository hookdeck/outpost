// Run with: node generate_test.js > match_test.go

// Helper to check if a test uses $ref
function usesRef(obj) {
  if (obj === null || typeof obj !== "object") return false;
  if (Array.isArray(obj)) return obj.some(usesRef);
  for (const key of Object.keys(obj)) {
    if (key === "$ref") return true;
    if (usesRef(obj[key])) return true;
  }
  return false;
}

const tests = [
  [{ type: "created" }, { type: "created" }, true],
  [{}, { type: "created" }, false],
  [{ type: "updated" }, { type: "created" }, false],
  [{ type: 1 }, { type: "created" }, false],
  [{ type: 1 }, { type: 1 }, true],
  [{ count: 1, type: "created" }, { count: 1 }, true],
  [{ count: 1, type: "created" }, { count: 1, type: "created" }, true],
  [{ count: 1 }, { count: 1, type: "created" }, false],
  [{ count: 0 }, { count: { $lt: 1 } }, true],
  [{ count: 2 }, { count: { $lt: 1 } }, false],
  [{ count: 2 }, { count: { $eq: 2 } }, true],
  [{ count: 2 }, { count: { $neq: 2 } }, false],
  [{ count: 2 }, { count: { $gt: 1, $lt: 3 } }, true],
  [{ count: 4 }, { count: { $gt: 1, $lt: 3 } }, false],
  [{ title: "a" }, { title: { $gt: "b" } }, false],
  [{ title: "c" }, { title: { $gt: "b" } }, true],
  [{ type: "created" }, { type: { $neq: "created" } }, false],
  [{ type: "created" }, { type: { $eq: "created" } }, true],
  [{ type: { something: "created" } }, { type: { something: "created" } }, true],
  [{ type: { something: "created" } }, { type: { something: "updated" } }, false],
  [{ type: { something: "created" } }, { type: 1 }, false],
  [{ tags: ["test", "other"] }, { tags: "test" }, true],
  [{ tags: ["test", "other"] }, { tags: "nope" }, false],
  [{ items: [{ sku: "test" }] }, { items: { sku: "test" } }, true],
  [{ items: [{ sku: "test" }] }, { items: { sku: "1" } }, false],
  [{ items: [{ inventory: 9 }, { inventory: 11 }] }, { items: { inventory: { $lte: 10 } } }, true],
  [{ items: [{ inventory: 12 }, { inventory: 11 }] }, { items: { inventory: { $lte: 10 } } }, false],
  [{ tags: ["test", "other", "more"] }, { tags: ["test", "other"] }, true],
  [{ tags: ["test", "other", "more"] }, { tags: ["test", "whatever"] }, false],
  [{ tags: ["test", "other"] }, { tags: { $eq: ["test", "other"] } }, true],
  [{ tags: ["test", "other", "more"] }, { tags: { $eq: ["test", "other"] } }, false],
  [[1, 2, 3], 3, true],
  [[1, 2, 3], 4, false],
  [[1, 2, 3], [{ $eq: 3 }], true],
  [[1, 2, 3], [{ $eq: 4 }], false],
  [[1, 2, 3], { $eq: 3 }, false],
  [{ exist: true }, { exist: true }, true],
  [{ exist: true }, { exist: false }, false],
  [{ exist: null }, { exist: null }, true],
  [{ exist: null }, { exist: false }, false],
  [{ exist: null }, { exist: { $eq: null } }, true],
  [{ exist: null }, { exist: { $neq: null } }, false],
  ["created", "created", true],
  [1, 2, false],
  [10, { $gte: 5 }, true],
  [{ test: true }, true, false],
  [{ test: "some-text" }, { test: { $startsWith: "some" } }, true],
  [{ test: "some-text" }, { test: { $endsWith: "some" } }, false],
  [{ test: "some-text" }, { test: { $endsWith: "text" } }, true],
  [{ test: "some-text" }, { test: { something: "text" } }, false],
  [{ test: { more: true } }, { test: { $startsWith: "text" } }, false],
  [{ test: "some-text" }, { test: { $startsWith: ["some", "else"] } }, true],
  [{ test: "some-text" }, { test: { $startsWith: ["something", "else"] } }, false],
  [{ test: "some-text" }, { test: { $endsWith: ["some", "else"] } }, false],
  [{ test: "some-text" }, { test: { $endsWith: ["text", "else"] } }, true],
  [{ test: "some-text", id: 123 }, { test: { $in: "text" }, id: { $in: [123, 456] } }, true],
  [{ test: "some-text", id: 123 }, { test: { $in: ["some-text", "other-text"] }, id: { $in: [123, 456] } }, true],
  [{ test: "some-text", id: 123 }, { test: { $in: ["some", "text"] }, id: { $nin: [123, 456] } }, false],
  [{ test: "some-text" }, { test: { $in: "text" } }, true],
  [{ test: "some-text" }, { test: { $nin: "some" } }, false],
  [{ tags: ["test", "something"] }, { tags: { $nin: "test" } }, false],
  [{ test: true, test2: true }, { test2: { $ref: "test" } }, true],
  [{ test: true, test2: false }, { test2: { $ref: "test" } }, false],
  [{ test: 1, test2: 2 }, { test2: { $gt: { $ref: "test" } } }, true],
  [{ types: ["something", "else"], test2: "else" }, { types: { $ref: "test2" } }, true],
  [{ types: ["something", "else"], test2: "else" }, { test2: { $ref: "types[1]" } }, true],
  [{ current: { something: true }, another: { thing: true } }, { another: { thing: { $ref: "current.something" } } }, true],
  [{ current: { something: true }, another: { thing: true } }, { another: { thing: { $ref: { bad: "ref" } } } }, false],
  [{ test: [{ a: 2, b: 1 }, { a: 2, b: 2 }] }, { test: { a: { $eq: { $ref: "test[$index].b" } } } }, true],
  [{ test: [{ a: 2, b: 1 }, { a: 2, b: 2 }] }, { $or: [{ test: { a: { $eq: { $ref: "test[$index].b" } } } }] }, true],
  [{ test: [{ a: [{ b: 3, c: 3 }] }, { a: [{ b: 2, c: 3 }] }] }, { test: { a: { b: { $ref: "test[$index].a[$index].c" } } } }, true],
  [{ test: [{ a: [{ b: 3, c: 4 }] }, { a: [{ b: 2, c: 3 }] }] }, { test: { a: { b: { $ref: "test[$index].a[$index].c" } } } }, false],
  [[{ a: 2, b: 1 }, { a: 2, b: 2 }], { a: { $eq: { $ref: "[$index].b" } } }, true],
  [{ test: 1 }, { test: { $gt: [1, 2, 3] } }, false],
  [{ test: true }, { $or: [{ test: true }] }, true],
  [{ test: true }, { $or: [{ test: false }] }, false],
  [{ test: { something: "else" } }, { test: { $or: [{ something: true }, { something: { $in: "else" } }] } }, true],
  [{ test: { something: "else" } }, { test: { $or: [{ something: true }, { something: { $in: "no" } }] } }, false],
  [1, { $or: [1, 2] }, true],
  [1, { $or: [2, 3] }, false],
  [{ test: true }, { $and: [{ test: true }] }, true],
  [{ test: true }, { $or: [{ test: false }] }, false],
  [{ test: { something: "else" } }, { test: { $and: [{ something: { $neq: null } }, { something: { $in: "else" } }] } }, true],
  [{ test: { something: null } }, { test: { $and: [{ something: { $neq: null } }, { something: { $in: "else" } }] } }, false],
  [1, { $and: [1, 2] }, false],
  [{ current: { a: "a" }, previous: { a: "test" } }, { current: { $and: [{ a: { $neq: null } }, { a: { $neq: { $ref: "previous.a" } } }] } }, true],
  [{ test: "else" }, { test: { $exist: true } }, true],
  [{ test: "else" }, { test: { $exist: false } }, false],
  [{ test1: "else" }, { test: { $exist: true } }, false],
  [{ test1: "else" }, { test: { $exist: false } }, true],
  ["/test", "/test", true],
  ["/test", "/test2", false],
  [1, 1, true],
  [1, 2, false],
  [1, {}, false],
  [{ test: { test1: "else" } }, { test: { test1: { $exist: true, $or: ["else", "not"] } } }, true],
  [{ test: { test1: "else1" } }, { test: { test1: { $exist: true, $or: ["else", "not"] } } }, false],
  [{ test: { test1: "else" } }, { test: { test1: { $exist: true, $in: "el" } } }, true],
  [{ test: { test1: "else" } }, { test: { test1: { $exist: true, $in: "no" } } }, false],
  [{ test: { test1: "else", test2: "not" } }, { test: { test1: { $exist: true, $or: ["else", "not"] }, $and: [{ test1: "else" }, { test2: "not" }] } }, true],
  [{ test: { test1: "else", test2: "not" } }, { test: { test1: { $exist: true, $or: ["else1", "not1"] }, $and: [{ test1: "else1" }, { test2: "not1" }] } }, false],
  [{ test: { test1: { test2: "else" } } }, { test: { test1: { test2: { $exist: true } } } }, true],
  [{ test: { test1: { test2: "else" } } }, { test: { test1: { test2: { $exist: false } } } }, false],
  [{ test: { test1: { test3: "else" } } }, { test: { test1: { test2: { $exist: false } } } }, true],
  [{ test: { test1: { test3: "else" } } }, { test: { test1: { test2: { $exist: true } } } }, false],
  [{ test: { test1: "else" } }, { $or: [{ test: { test1: { $exist: true } } }, { test: { test1: "else1" } }] }, true],
  [{ test: { test1: "else" } }, { $or: [{ test: { test1: { $exist: false } } }, { test: { test1: "else" } }] }, true],
  [{ test: { test2: "else" } }, { $or: [{ test: { test1: { $exist: true } } }, { test: { test2: "else" } }] }, true],
  [{ test: { test2: "else" } }, { $or: [{ test: { test1: { $exist: true } } }, { test: { test2: "else1" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $or: [{ test: { test1: { $exist: true } } }, { test: { test2: "else1" } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $and: [{ test: { test1: { $exist: true } } }, { test: { test2: "not" } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $and: [{ test: { test1: { $exist: true } } }, { test: { test2: "not1" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $and: [{ test: { test1: { $exist: true } } }, { test: { test1: "else" } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $and: [{ test: { test1: { $exist: true } } }, { test: { test1: "else1" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $and: [{ test: { test1: { $exist: true, $eq: "else" } } }] }, true],
  [{ test: { test2: "not" } }, { $and: [{ test: { test1: { $exist: true, $eq: "not" } } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $and: [{ test: { test1: { $exist: true } } }, { test: { test2: { $exist: true } } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $and: [{ test: { test1: { $exist: true } } }, { test: { test2: { $exist: false } } }] }, false],
  [{ test: { test1: "else" } }, { $and: [{ test: { test1: { $exist: true } } }, { test: { test2: { $exist: false } } }] }, true],
  [{ test: { test1: "else" } }, { $or: [{ test: { test1: { $exist: true } } }, { test: { test2: { $exist: false } } }] }, true],
  [{ test: { test3: "else" } }, { $or: [{ test: { test1: { $exist: true } } }, { test: { test2: { $exist: false } } }] }, true],
];

const not_tests = [
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else2" } }, $and: [{ test: { test1: "else" } }, { test: { test2: "not" } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else" } }, $and: [{ test: { test1: "else" } }, { test: { test2: "not" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: { $exist: true } } }, $and: [{ test: { test1: "else" } }, { test: { test2: "not" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: { $exist: false } } }, $and: [{ test: { test1: "else" } }, { test: { test2: "not" } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: { $exist: false } } }, $and: [{ test: { test3: { $exist: false } } }, { test: { test2: "not" } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: { $exist: false } } }, $and: [{ test: { test3: { $exist: true } } }, { test: { test2: "not" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else2" } }, $or: [{ test: { test3: { $exist: true } } }, { test: { test2: "not" } }] }, true],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else" } }, $or: [{ test: { test3: { $exist: true } } }, { test: { test2: "not" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else" } }, $or: [{ test: { test3: { $exist: false } } }, { test: { test2: "not" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else" } }, $or: [{ test: { test3: { $exist: false } } }, { test: { test2: "not2" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else2" } }, $or: [{ test: { test3: { $exist: true } } }, { test: { test2: "not2" } }] }, false],
  [{ test: { test1: "else", test2: "not" } }, { $not: { test: { test1: "else2" } }, $or: [{ test: { test3: { $exist: false } } }, { test: { test2: "not" } }] }, true],
];

// Convert JS value to Go literal
function toGo(val, indent = "\t\t") {
  if (val === null) {
    return "nil";
  }
  if (typeof val === "boolean") {
    return val.toString();
  }
  if (typeof val === "number") {
    return `float64(${val})`;
  }
  if (typeof val === "string") {
    return `"${val.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
  }
  if (Array.isArray(val)) {
    if (val.length === 0) {
      return "[]any{}";
    }
    const items = val.map((v) => toGo(v, indent + "\t")).join(", ");
    return `[]any{${items}}`;
  }
  if (typeof val === "object") {
    const keys = Object.keys(val);
    if (keys.length === 0) {
      return "map[string]any{}";
    }
    const pairs = keys.map((k) => `"${k}": ${toGo(val[k], indent + "\t")}`).join(", ");
    return `map[string]any{${pairs}}`;
  }
  return "nil";
}

// Separate tests into implemented and skipped ($ref)
const implementedTests = [];
const skippedTests = [];

tests.forEach(([input, schema, expected], i) => {
  if (usesRef(schema)) {
    skippedTests.push({ input, schema, expected, originalIndex: i, reason: "$ref not implemented" });
  } else {
    implementedTests.push({ input, schema, expected, originalIndex: i });
  }
});

// Generate Go test file
console.log(`package simplejsonmatch

import (
\t"encoding/json"
\t"fmt"
\t"testing"
)

// TestMatch runs all test cases ported from the original simple-json-match library.
// Source: https://github.com/hookdeck/simple-json-match/blob/main/src/tests/index.test.ts
// Total: ${tests.length} main tests (${implementedTests.length} implemented, ${skippedTests.length} skipped) + ${not_tests.length} not_tests
func TestMatch(t *testing.T) {
\ttests := []struct {
\t\tinput    any
\t\tschema   any
\t\texpected bool
\t}{`);

implementedTests.forEach(({ input, schema, expected, originalIndex }) => {
  console.log(`\t\t// ${originalIndex}`);
  console.log(`\t\t{${toGo(input)}, ${toGo(schema)}, ${expected}},`);
});

console.log(`\t}

\tfor i, tt := range tests {
\t\tt.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
\t\t\tresult := Match(tt.input, tt.schema)
\t\t\tif result != tt.expected {
\t\t\t\tinputJSON, _ := json.Marshal(tt.input)
\t\t\t\tschemaJSON, _ := json.Marshal(tt.schema)
\t\t\t\tt.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, tt.expected)
\t\t\t}
\t\t})

\t\t// Also test with $not wrapper - should invert the result
\t\tt.Run(fmt.Sprintf("case_%d_with_not", i), func(t *testing.T) {
\t\t\tnotSchema := map[string]any{"$not": tt.schema}
\t\t\tresult := Match(tt.input, notSchema)
\t\t\texpectedInverted := !tt.expected
\t\t\tif result != expectedInverted {
\t\t\t\tinputJSON, _ := json.Marshal(tt.input)
\t\t\t\tschemaJSON, _ := json.Marshal(notSchema)
\t\t\t\tt.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, expectedInverted)
\t\t\t}
\t\t})
\t}
}

// TestMatchRefSkipped contains test cases for $ref operator which is not implemented.
// These tests are skipped but kept for documentation and future implementation.
func TestMatchRefSkipped(t *testing.T) {
\ttests := []struct {
\t\tinput    any
\t\tschema   any
\t\texpected bool
\t}{`);

skippedTests.forEach(({ input, schema, expected, originalIndex }) => {
  console.log(`\t\t// original index: ${originalIndex}`);
  console.log(`\t\t{${toGo(input)}, ${toGo(schema)}, ${expected}},`);
});

console.log(`\t}

\tfor i, tt := range tests {
\t\tt.Run(fmt.Sprintf("ref_case_%d", i), func(t *testing.T) {
\t\t\tt.Skip("$ref operator not implemented")
\t\t\tresult := Match(tt.input, tt.schema)
\t\t\tif result != tt.expected {
\t\t\t\tinputJSON, _ := json.Marshal(tt.input)
\t\t\t\tschemaJSON, _ := json.Marshal(tt.schema)
\t\t\t\tt.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, tt.expected)
\t\t\t}
\t\t})
\t}
}

// TestMatchNot tests $not operator cases from the original library.
func TestMatchNot(t *testing.T) {
\ttests := []struct {
\t\tinput    any
\t\tschema   any
\t\texpected bool
\t}{`);

not_tests.forEach(([input, schema, expected], i) => {
  console.log(`\t\t// ${i}`);
  console.log(`\t\t{${toGo(input)}, ${toGo(schema)}, ${expected}},`);
});

console.log(`\t}

\tfor i, tt := range tests {
\t\tt.Run(fmt.Sprintf("not_case_%d", i), func(t *testing.T) {
\t\t\tresult := Match(tt.input, tt.schema)
\t\t\tif result != tt.expected {
\t\t\t\tinputJSON, _ := json.Marshal(tt.input)
\t\t\t\tschemaJSON, _ := json.Marshal(tt.schema)
\t\t\t\tt.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, tt.expected)
\t\t\t}
\t\t})
\t}
}
`);

package simplejsonmatch

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestMatch runs all test cases ported from the original simple-json-match library.
// Source: https://github.com/hookdeck/simple-json-match/blob/main/src/tests/index.test.ts
// Total: 121 main tests (108 implemented, 13 skipped) + 12 not_tests
func TestMatch(t *testing.T) {
	tests := []struct {
		input    any
		schema   any
		expected bool
	}{
		// 0
		{map[string]any{"type": "created"}, map[string]any{"type": "created"}, true},
		// 1
		{map[string]any{}, map[string]any{"type": "created"}, false},
		// 2
		{map[string]any{"type": "updated"}, map[string]any{"type": "created"}, false},
		// 3
		{map[string]any{"type": float64(1)}, map[string]any{"type": "created"}, false},
		// 4
		{map[string]any{"type": float64(1)}, map[string]any{"type": float64(1)}, true},
		// 5
		{map[string]any{"count": float64(1), "type": "created"}, map[string]any{"count": float64(1)}, true},
		// 6
		{map[string]any{"count": float64(1), "type": "created"}, map[string]any{"count": float64(1), "type": "created"}, true},
		// 7
		{map[string]any{"count": float64(1)}, map[string]any{"count": float64(1), "type": "created"}, false},
		// 8
		{map[string]any{"count": float64(0)}, map[string]any{"count": map[string]any{"$lt": float64(1)}}, true},
		// 9
		{map[string]any{"count": float64(2)}, map[string]any{"count": map[string]any{"$lt": float64(1)}}, false},
		// 10
		{map[string]any{"count": float64(2)}, map[string]any{"count": map[string]any{"$eq": float64(2)}}, true},
		// 11
		{map[string]any{"count": float64(2)}, map[string]any{"count": map[string]any{"$neq": float64(2)}}, false},
		// 12
		{map[string]any{"count": float64(2)}, map[string]any{"count": map[string]any{"$gt": float64(1), "$lt": float64(3)}}, true},
		// 13
		{map[string]any{"count": float64(4)}, map[string]any{"count": map[string]any{"$gt": float64(1), "$lt": float64(3)}}, false},
		// 14
		{map[string]any{"title": "a"}, map[string]any{"title": map[string]any{"$gt": "b"}}, false},
		// 15
		{map[string]any{"title": "c"}, map[string]any{"title": map[string]any{"$gt": "b"}}, true},
		// 16
		{map[string]any{"type": "created"}, map[string]any{"type": map[string]any{"$neq": "created"}}, false},
		// 17
		{map[string]any{"type": "created"}, map[string]any{"type": map[string]any{"$eq": "created"}}, true},
		// 18
		{map[string]any{"type": map[string]any{"something": "created"}}, map[string]any{"type": map[string]any{"something": "created"}}, true},
		// 19
		{map[string]any{"type": map[string]any{"something": "created"}}, map[string]any{"type": map[string]any{"something": "updated"}}, false},
		// 20
		{map[string]any{"type": map[string]any{"something": "created"}}, map[string]any{"type": float64(1)}, false},
		// 21
		{map[string]any{"tags": []any{"test", "other"}}, map[string]any{"tags": "test"}, true},
		// 22
		{map[string]any{"tags": []any{"test", "other"}}, map[string]any{"tags": "nope"}, false},
		// 23
		{map[string]any{"items": []any{map[string]any{"sku": "test"}}}, map[string]any{"items": map[string]any{"sku": "test"}}, true},
		// 24
		{map[string]any{"items": []any{map[string]any{"sku": "test"}}}, map[string]any{"items": map[string]any{"sku": "1"}}, false},
		// 25
		{map[string]any{"items": []any{map[string]any{"inventory": float64(9)}, map[string]any{"inventory": float64(11)}}}, map[string]any{"items": map[string]any{"inventory": map[string]any{"$lte": float64(10)}}}, true},
		// 26
		{map[string]any{"items": []any{map[string]any{"inventory": float64(12)}, map[string]any{"inventory": float64(11)}}}, map[string]any{"items": map[string]any{"inventory": map[string]any{"$lte": float64(10)}}}, false},
		// 27
		{map[string]any{"tags": []any{"test", "other", "more"}}, map[string]any{"tags": []any{"test", "other"}}, true},
		// 28
		{map[string]any{"tags": []any{"test", "other", "more"}}, map[string]any{"tags": []any{"test", "whatever"}}, false},
		// 29
		{map[string]any{"tags": []any{"test", "other"}}, map[string]any{"tags": map[string]any{"$eq": []any{"test", "other"}}}, true},
		// 30
		{map[string]any{"tags": []any{"test", "other", "more"}}, map[string]any{"tags": map[string]any{"$eq": []any{"test", "other"}}}, false},
		// 31
		{[]any{float64(1), float64(2), float64(3)}, float64(3), true},
		// 32
		{[]any{float64(1), float64(2), float64(3)}, float64(4), false},
		// 33
		{[]any{float64(1), float64(2), float64(3)}, []any{map[string]any{"$eq": float64(3)}}, true},
		// 34
		{[]any{float64(1), float64(2), float64(3)}, []any{map[string]any{"$eq": float64(4)}}, false},
		// 35
		{[]any{float64(1), float64(2), float64(3)}, map[string]any{"$eq": float64(3)}, false},
		// 36
		{map[string]any{"exist": true}, map[string]any{"exist": true}, true},
		// 37
		{map[string]any{"exist": true}, map[string]any{"exist": false}, false},
		// 38
		{map[string]any{"exist": nil}, map[string]any{"exist": nil}, true},
		// 39
		{map[string]any{"exist": nil}, map[string]any{"exist": false}, false},
		// 40
		{map[string]any{"exist": nil}, map[string]any{"exist": map[string]any{"$eq": nil}}, true},
		// 41
		{map[string]any{"exist": nil}, map[string]any{"exist": map[string]any{"$neq": nil}}, false},
		// 42
		{"created", "created", true},
		// 43
		{float64(1), float64(2), false},
		// 44
		{float64(10), map[string]any{"$gte": float64(5)}, true},
		// 45
		{map[string]any{"test": true}, true, false},
		// 46
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$startsWith": "some"}}, true},
		// 47
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$endsWith": "some"}}, false},
		// 48
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$endsWith": "text"}}, true},
		// 49
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"something": "text"}}, false},
		// 50
		{map[string]any{"test": map[string]any{"more": true}}, map[string]any{"test": map[string]any{"$startsWith": "text"}}, false},
		// 51
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$startsWith": []any{"some", "else"}}}, true},
		// 52
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$startsWith": []any{"something", "else"}}}, false},
		// 53
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$endsWith": []any{"some", "else"}}}, false},
		// 54
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$endsWith": []any{"text", "else"}}}, true},
		// 55
		{map[string]any{"test": "some-text", "id": float64(123)}, map[string]any{"test": map[string]any{"$in": "text"}, "id": map[string]any{"$in": []any{float64(123), float64(456)}}}, true},
		// 56
		{map[string]any{"test": "some-text", "id": float64(123)}, map[string]any{"test": map[string]any{"$in": []any{"some-text", "other-text"}}, "id": map[string]any{"$in": []any{float64(123), float64(456)}}}, true},
		// 57
		{map[string]any{"test": "some-text", "id": float64(123)}, map[string]any{"test": map[string]any{"$in": []any{"some", "text"}}, "id": map[string]any{"$nin": []any{float64(123), float64(456)}}}, false},
		// 58
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$in": "text"}}, true},
		// 59
		{map[string]any{"test": "some-text"}, map[string]any{"test": map[string]any{"$nin": "some"}}, false},
		// 60
		{map[string]any{"tags": []any{"test", "something"}}, map[string]any{"tags": map[string]any{"$nin": "test"}}, false},
		// 73
		{map[string]any{"test": float64(1)}, map[string]any{"test": map[string]any{"$gt": []any{float64(1), float64(2), float64(3)}}}, false},
		// 74
		{map[string]any{"test": true}, map[string]any{"$or": []any{map[string]any{"test": true}}}, true},
		// 75
		{map[string]any{"test": true}, map[string]any{"$or": []any{map[string]any{"test": false}}}, false},
		// 76
		{map[string]any{"test": map[string]any{"something": "else"}}, map[string]any{"test": map[string]any{"$or": []any{map[string]any{"something": true}, map[string]any{"something": map[string]any{"$in": "else"}}}}}, true},
		// 77
		{map[string]any{"test": map[string]any{"something": "else"}}, map[string]any{"test": map[string]any{"$or": []any{map[string]any{"something": true}, map[string]any{"something": map[string]any{"$in": "no"}}}}}, false},
		// 78
		{float64(1), map[string]any{"$or": []any{float64(1), float64(2)}}, true},
		// 79
		{float64(1), map[string]any{"$or": []any{float64(2), float64(3)}}, false},
		// 80
		{map[string]any{"test": true}, map[string]any{"$and": []any{map[string]any{"test": true}}}, true},
		// 81
		{map[string]any{"test": true}, map[string]any{"$or": []any{map[string]any{"test": false}}}, false},
		// 82
		{map[string]any{"test": map[string]any{"something": "else"}}, map[string]any{"test": map[string]any{"$and": []any{map[string]any{"something": map[string]any{"$neq": nil}}, map[string]any{"something": map[string]any{"$in": "else"}}}}}, true},
		// 83
		{map[string]any{"test": map[string]any{"something": nil}}, map[string]any{"test": map[string]any{"$and": []any{map[string]any{"something": map[string]any{"$neq": nil}}, map[string]any{"something": map[string]any{"$in": "else"}}}}}, false},
		// 84
		{float64(1), map[string]any{"$and": []any{float64(1), float64(2)}}, false},
		// 86
		{map[string]any{"test": "else"}, map[string]any{"test": map[string]any{"$exist": true}}, true},
		// 87
		{map[string]any{"test": "else"}, map[string]any{"test": map[string]any{"$exist": false}}, false},
		// 88
		{map[string]any{"test1": "else"}, map[string]any{"test": map[string]any{"$exist": true}}, false},
		// 89
		{map[string]any{"test1": "else"}, map[string]any{"test": map[string]any{"$exist": false}}, true},
		// 90
		{"/test", "/test", true},
		// 91
		{"/test", "/test2", false},
		// 92
		{float64(1), float64(1), true},
		// 93
		{float64(1), float64(2), false},
		// 94
		{float64(1), map[string]any{}, false},
		// 95
		{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$or": []any{"else", "not"}}}}, true},
		// 96
		{map[string]any{"test": map[string]any{"test1": "else1"}}, map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$or": []any{"else", "not"}}}}, false},
		// 97
		{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$in": "el"}}}, true},
		// 98
		{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$in": "no"}}}, false},
		// 99
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$or": []any{"else", "not"}}, "$and": []any{map[string]any{"test1": "else"}, map[string]any{"test2": "not"}}}}, true},
		// 100
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$or": []any{"else1", "not1"}}, "$and": []any{map[string]any{"test1": "else1"}, map[string]any{"test2": "not1"}}}}, false},
		// 101
		{map[string]any{"test": map[string]any{"test1": map[string]any{"test2": "else"}}}, map[string]any{"test": map[string]any{"test1": map[string]any{"test2": map[string]any{"$exist": true}}}}, true},
		// 102
		{map[string]any{"test": map[string]any{"test1": map[string]any{"test2": "else"}}}, map[string]any{"test": map[string]any{"test1": map[string]any{"test2": map[string]any{"$exist": false}}}}, false},
		// 103
		{map[string]any{"test": map[string]any{"test1": map[string]any{"test3": "else"}}}, map[string]any{"test": map[string]any{"test1": map[string]any{"test2": map[string]any{"$exist": false}}}}, true},
		// 104
		{map[string]any{"test": map[string]any{"test1": map[string]any{"test3": "else"}}}, map[string]any{"test": map[string]any{"test1": map[string]any{"test2": map[string]any{"$exist": true}}}}, false},
		// 105
		{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test1": "else1"}}}}, true},
		// 106
		{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": false}}}, map[string]any{"test": map[string]any{"test1": "else"}}}}, true},
		// 107
		{map[string]any{"test": map[string]any{"test2": "else"}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "else"}}}}, true},
		// 108
		{map[string]any{"test": map[string]any{"test2": "else"}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "else1"}}}}, false},
		// 109
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "else1"}}}}, true},
		// 110
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, true},
		// 111
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "not1"}}}}, false},
		// 112
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test1": "else"}}}}, true},
		// 113
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test1": "else1"}}}}, false},
		// 114
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$eq": "else"}}}}}, true},
		// 115
		{map[string]any{"test": map[string]any{"test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true, "$eq": "not"}}}}}, false},
		// 116
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": map[string]any{"$exist": true}}}}}, true},
		// 117
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": map[string]any{"$exist": false}}}}}, false},
		// 118
		{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"$and": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": map[string]any{"$exist": false}}}}}, true},
		// 119
		{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": map[string]any{"$exist": false}}}}}, true},
		// 120
		{map[string]any{"test": map[string]any{"test3": "else"}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": map[string]any{"$exist": false}}}}}, true},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			result := Match(tt.input, tt.schema)
			if result != tt.expected {
				inputJSON, _ := json.Marshal(tt.input)
				schemaJSON, _ := json.Marshal(tt.schema)
				t.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, tt.expected)
			}
		})

		// Also test with $not wrapper - should invert the result
		t.Run(fmt.Sprintf("case_%d_with_not", i), func(t *testing.T) {
			notSchema := map[string]any{"$not": tt.schema}
			result := Match(tt.input, notSchema)
			expectedInverted := !tt.expected
			if result != expectedInverted {
				inputJSON, _ := json.Marshal(tt.input)
				schemaJSON, _ := json.Marshal(notSchema)
				t.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, expectedInverted)
			}
		})
	}
}

// TestMatchRefSkipped contains test cases for $ref operator which is not implemented.
// These tests are skipped but kept for documentation and future implementation.
func TestMatchRefSkipped(t *testing.T) {
	tests := []struct {
		input    any
		schema   any
		expected bool
	}{
		// original index: 61
		{map[string]any{"test": true, "test2": true}, map[string]any{"test2": map[string]any{"$ref": "test"}}, true},
		// original index: 62
		{map[string]any{"test": true, "test2": false}, map[string]any{"test2": map[string]any{"$ref": "test"}}, false},
		// original index: 63
		{map[string]any{"test": float64(1), "test2": float64(2)}, map[string]any{"test2": map[string]any{"$gt": map[string]any{"$ref": "test"}}}, true},
		// original index: 64
		{map[string]any{"types": []any{"something", "else"}, "test2": "else"}, map[string]any{"types": map[string]any{"$ref": "test2"}}, true},
		// original index: 65
		{map[string]any{"types": []any{"something", "else"}, "test2": "else"}, map[string]any{"test2": map[string]any{"$ref": "types[1]"}}, true},
		// original index: 66
		{map[string]any{"current": map[string]any{"something": true}, "another": map[string]any{"thing": true}}, map[string]any{"another": map[string]any{"thing": map[string]any{"$ref": "current.something"}}}, true},
		// original index: 67
		{map[string]any{"current": map[string]any{"something": true}, "another": map[string]any{"thing": true}}, map[string]any{"another": map[string]any{"thing": map[string]any{"$ref": map[string]any{"bad": "ref"}}}}, false},
		// original index: 68
		{map[string]any{"test": []any{map[string]any{"a": float64(2), "b": float64(1)}, map[string]any{"a": float64(2), "b": float64(2)}}}, map[string]any{"test": map[string]any{"a": map[string]any{"$eq": map[string]any{"$ref": "test[$index].b"}}}}, true},
		// original index: 69
		{map[string]any{"test": []any{map[string]any{"a": float64(2), "b": float64(1)}, map[string]any{"a": float64(2), "b": float64(2)}}}, map[string]any{"$or": []any{map[string]any{"test": map[string]any{"a": map[string]any{"$eq": map[string]any{"$ref": "test[$index].b"}}}}}}, true},
		// original index: 70
		{map[string]any{"test": []any{map[string]any{"a": []any{map[string]any{"b": float64(3), "c": float64(3)}}}, map[string]any{"a": []any{map[string]any{"b": float64(2), "c": float64(3)}}}}}, map[string]any{"test": map[string]any{"a": map[string]any{"b": map[string]any{"$ref": "test[$index].a[$index].c"}}}}, true},
		// original index: 71
		{map[string]any{"test": []any{map[string]any{"a": []any{map[string]any{"b": float64(3), "c": float64(4)}}}, map[string]any{"a": []any{map[string]any{"b": float64(2), "c": float64(3)}}}}}, map[string]any{"test": map[string]any{"a": map[string]any{"b": map[string]any{"$ref": "test[$index].a[$index].c"}}}}, false},
		// original index: 72
		{[]any{map[string]any{"a": float64(2), "b": float64(1)}, map[string]any{"a": float64(2), "b": float64(2)}}, map[string]any{"a": map[string]any{"$eq": map[string]any{"$ref": "[$index].b"}}}, true},
		// original index: 85
		{map[string]any{"current": map[string]any{"a": "a"}, "previous": map[string]any{"a": "test"}}, map[string]any{"current": map[string]any{"$and": []any{map[string]any{"a": map[string]any{"$neq": nil}}, map[string]any{"a": map[string]any{"$neq": map[string]any{"$ref": "previous.a"}}}}}}, true},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("ref_case_%d", i), func(t *testing.T) {
			t.Skip("$ref operator not implemented")
			result := Match(tt.input, tt.schema)
			if result != tt.expected {
				inputJSON, _ := json.Marshal(tt.input)
				schemaJSON, _ := json.Marshal(tt.schema)
				t.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, tt.expected)
			}
		})
	}
}

// TestMatchNot tests $not operator cases from the original library.
func TestMatchNot(t *testing.T) {
	tests := []struct {
		input    any
		schema   any
		expected bool
	}{
		// 0
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else2"}}, "$and": []any{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, true},
		// 1
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else"}}, "$and": []any{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, false},
		// 2
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": true}}}, "$and": []any{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, false},
		// 3
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": false}}}, "$and": []any{map[string]any{"test": map[string]any{"test1": "else"}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, true},
		// 4
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": false}}}, "$and": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": false}}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, true},
		// 5
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": map[string]any{"$exist": false}}}, "$and": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, false},
		// 6
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else2"}}, "$or": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, true},
		// 7
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else"}}, "$or": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, false},
		// 8
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else"}}, "$or": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": false}}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, false},
		// 9
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else"}}, "$or": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": false}}}, map[string]any{"test": map[string]any{"test2": "not2"}}}}, false},
		// 10
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else2"}}, "$or": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": true}}}, map[string]any{"test": map[string]any{"test2": "not2"}}}}, false},
		// 11
		{map[string]any{"test": map[string]any{"test1": "else", "test2": "not"}}, map[string]any{"$not": map[string]any{"test": map[string]any{"test1": "else2"}}, "$or": []any{map[string]any{"test": map[string]any{"test3": map[string]any{"$exist": false}}}, map[string]any{"test": map[string]any{"test2": "not"}}}}, true},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("not_case_%d", i), func(t *testing.T) {
			result := Match(tt.input, tt.schema)
			if result != tt.expected {
				inputJSON, _ := json.Marshal(tt.input)
				schemaJSON, _ := json.Marshal(tt.schema)
				t.Errorf("Match(%s, %s) = %v, want %v", inputJSON, schemaJSON, result, tt.expected)
			}
		})
	}
}

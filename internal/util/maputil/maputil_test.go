package maputil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergePatchStringMap(t *testing.T) {
	t.Run("add new key preserves existing", func(t *testing.T) {
		result := MergePatchStringMap(
			map[string]string{"a": "1"},
			map[string]any{"b": "2"},
		)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, result)
	})

	t.Run("update existing key", func(t *testing.T) {
		result := MergePatchStringMap(
			map[string]string{"a": "1"},
			map[string]any{"a": "2"},
		)
		assert.Equal(t, map[string]string{"a": "2"}, result)
	})

	t.Run("delete key via null", func(t *testing.T) {
		result := MergePatchStringMap(
			map[string]string{"a": "1", "b": "2"},
			map[string]any{"a": nil},
		)
		assert.Equal(t, map[string]string{"b": "2"}, result)
	})

	t.Run("delete nonexistent key is no-op", func(t *testing.T) {
		result := MergePatchStringMap(
			map[string]string{"a": "1"},
			map[string]any{"z": nil},
		)
		assert.Equal(t, map[string]string{"a": "1"}, result)
	})

	t.Run("delete all keys returns nil", func(t *testing.T) {
		result := MergePatchStringMap(
			map[string]string{"a": "1"},
			map[string]any{"a": nil},
		)
		assert.Nil(t, result)
	})

	t.Run("mixed add update delete", func(t *testing.T) {
		result := MergePatchStringMap(
			map[string]string{"keep": "v", "remove": "v", "update": "old"},
			map[string]any{"remove": nil, "update": "new", "add": "v"},
		)
		assert.Equal(t, map[string]string{"keep": "v", "update": "new", "add": "v"}, result)
	})

	t.Run("nil original treated as empty", func(t *testing.T) {
		result := MergePatchStringMap(
			nil,
			map[string]any{"a": "1"},
		)
		assert.Equal(t, map[string]string{"a": "1"}, result)
	})

	t.Run("empty patch is no-op", func(t *testing.T) {
		result := MergePatchStringMap(
			map[string]string{"a": "1"},
			map[string]any{},
		)
		assert.Equal(t, map[string]string{"a": "1"}, result)
	})

	t.Run("nil original and empty patch returns nil", func(t *testing.T) {
		result := MergePatchStringMap(nil, map[string]any{})
		assert.Nil(t, result)
	})

	t.Run("type coercion float64", func(t *testing.T) {
		result := MergePatchStringMap(
			nil,
			map[string]any{"num": float64(42)},
		)
		assert.Equal(t, map[string]string{"num": "42"}, result)
	})

	t.Run("type coercion bool", func(t *testing.T) {
		result := MergePatchStringMap(
			nil,
			map[string]any{"flag": true},
		)
		assert.Equal(t, map[string]string{"flag": "true"}, result)
	})

	t.Run("type coercion float with decimal", func(t *testing.T) {
		result := MergePatchStringMap(
			nil,
			map[string]any{"pi": float64(3.14)},
		)
		assert.Equal(t, map[string]string{"pi": "3.14"}, result)
	})
}

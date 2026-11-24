package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestHeaderTemplates(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		var h HeaderTemplates
		require.NoError(t, h.UnmarshalText([]byte(`a={{.EventID}}, b = x\,y , c=k=v`)))
		assert.Equal(t, HeaderTemplates{"a": "{{.EventID}}", "b": "x,y", "c": "k=v"}, h)
	})
	t.Run("text empty", func(t *testing.T) {
		var h HeaderTemplates
		require.NoError(t, h.UnmarshalText([]byte("  ")))
		assert.Nil(t, h)
	})
	t.Run("text invalid", func(t *testing.T) {
		var h HeaderTemplates
		assert.Error(t, h.UnmarshalText([]byte("a")))
		assert.Error(t, h.UnmarshalText([]byte("=x")))
	})
	t.Run("yaml map", func(t *testing.T) {
		var h HeaderTemplates
		require.NoError(t, yaml.Unmarshal([]byte("a: '{{.EventID}}'\nb: x,y\n"), &h))
		assert.Equal(t, HeaderTemplates{"a": "{{.EventID}}", "b": "x,y"}, h)
	})
	t.Run("yaml string", func(t *testing.T) {
		var h HeaderTemplates
		require.NoError(t, yaml.Unmarshal([]byte(`"a={{.EventID}},b=x\\,y"`), &h))
		assert.Equal(t, HeaderTemplates{"a": "{{.EventID}}", "b": "x,y"}, h)
	})
}

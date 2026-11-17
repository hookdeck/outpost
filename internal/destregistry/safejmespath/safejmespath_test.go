package safejmespath_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/safejmespath"
	"github.com/jmespath/go-jmespath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearch_PanicsBecomeErrors(t *testing.T) {
	t.Parallel()

	data := map[string]interface{}{"data": map[string]interface{}{"a": "b"}}

	tests := []struct {
		name       string
		expression string
	}{
		{"merge over an array", `to_string(merge([{a: data}]))`},
		{"contains with an object", "contains(`[{}]`, `{}`)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			compiled, err := jmespath.Compile(tt.expression)
			require.NoError(t, err)

			require.NotPanics(t, func() {
				_, err = safejmespath.Search(tt.expression, data)
			})
			assert.ErrorIs(t, err, safejmespath.ErrPanic)

			require.NotPanics(t, func() {
				_, err = safejmespath.SearchCompiled(compiled, data)
			})
			assert.ErrorIs(t, err, safejmespath.ErrPanic)
		})
	}
}

func TestSearch_Result(t *testing.T) {
	t.Parallel()

	data := map[string]interface{}{"data": map[string]interface{}{"id": "u_1"}}

	got, err := safejmespath.Search("data.id", data)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got)

	compiled, err := jmespath.Compile("data.id")
	require.NoError(t, err)
	got, err = safejmespath.SearchCompiled(compiled, data)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got)
}

func TestSearch_OrdinaryErrorIsNotPanic(t *testing.T) {
	t.Parallel()

	_, err := safejmespath.Search("length(data.missing)", map[string]interface{}{})
	require.Error(t, err)
	assert.NotErrorIs(t, err, safejmespath.ErrPanic)

	_, err = safejmespath.Search("[", nil)
	require.Error(t, err)
	assert.NotErrorIs(t, err, safejmespath.ErrPanic)
}

func TestTemplateError_Message(t *testing.T) {
	t.Parallel()

	_, panicErr := safejmespath.Search(`to_string(merge([{a: data}]))`, map[string]interface{}{})
	err := &safejmespath.TemplateError{Field: "key_template", Err: panicErr}
	assert.Equal(t, "key_template could not be evaluated: the expression failed on an unsupported value", err.Message())
	assert.Contains(t, err.Error(), "interface conversion", "the log form keeps the panic value")

	_, searchErr := safejmespath.Search("length(data.missing)", map[string]interface{}{})
	err = &safejmespath.TemplateError{Field: "partition_key_template", Err: searchErr}
	assert.Equal(t, "partition_key_template could not be evaluated: "+searchErr.Error(), err.Message())

	assert.Equal(t, err.Message(), safejmespath.MessageFor(fmt.Errorf("wrapped: %w", err)))
	assert.Empty(t, safejmespath.MessageFor(errors.New("other")))
}

package destregistry_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticLoader struct{ meta metadata.ProviderMetadata }

func (l staticLoader) Load(string) (*metadata.ProviderMetadata, error) {
	m := l.meta
	return &m, nil
}

func strptr(s string) *string { return &s }

// conditionalMetadata models a destination with two ways to authenticate:
// a select "auth" picks between "key" (default) and "token".
func conditionalMetadata() metadata.ProviderMetadata {
	return metadata.ProviderMetadata{
		Type: "test",
		ConfigFields: []metadata.FieldSchema{
			{Key: "name", Type: "text", Required: true},
			{Key: "auth", Type: "select", Default: strptr("key"), Options: []metadata.FieldOption{
				{Label: "Key", Value: "key"},
				{Label: "Token", Value: "token"},
			}},
			{Key: "audience", Type: "text", Required: true, VisibleWhen: &metadata.FieldCondition{Key: "auth", Values: []string{"token"}}},
		},
		CredentialFields: []metadata.FieldSchema{
			{Key: "key", Type: "text", Required: true, Sensitive: true, VisibleWhen: &metadata.FieldCondition{Key: "auth", Values: []string{"key"}}},
			{Key: "label", Type: "text", VisibleWhen: &metadata.FieldCondition{Key: "auth", Values: []string{"key", "token"}}},
		},
	}
}

func newConditionalProvider(t *testing.T) *destregistry.BaseProvider {
	t.Helper()
	p, err := destregistry.NewBaseProvider(staticLoader{conditionalMetadata()}, "test")
	require.NoError(t, err)
	return p
}

func validationErrors(t *testing.T, err error) []destregistry.ValidationErrorDetail {
	t.Helper()
	if err == nil {
		return nil
	}
	var verr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &verr)
	return verr.Errors
}

func TestBaseProvider_ValidateVisibleWhen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config map[string]string
		creds  map[string]string
		want   []destregistry.ValidationErrorDetail
	}{
		{
			name:   "controller missing evaluates as its default",
			config: map[string]string{"name": "a"},
			creds:  map[string]string{"key": "secret"},
		},
		{
			name:   "default branch requires its fields",
			config: map[string]string{"name": "a"},
			want:   []destregistry.ValidationErrorDetail{{Field: "credentials.key", Type: "required"}},
		},
		{
			name:   "other branch",
			config: map[string]string{"name": "a", "auth": "token", "audience": "x"},
		},
		{
			name:   "other branch requires its fields, not the default branch's",
			config: map[string]string{"name": "a", "auth": "token"},
			want:   []destregistry.ValidationErrorDetail{{Field: "config.audience", Type: "required"}},
		},
		{
			name:   "value for a hidden field",
			config: map[string]string{"name": "a", "auth": "token", "audience": "x"},
			creds:  map[string]string{"key": "secret"},
			want:   []destregistry.ValidationErrorDetail{{Field: "credentials.key", Type: "forbidden"}},
		},
		{
			name:   "value for a field hidden by the default",
			config: map[string]string{"name": "a", "audience": "x"},
			creds:  map[string]string{"key": "secret"},
			want:   []destregistry.ValidationErrorDetail{{Field: "config.audience", Type: "forbidden"}},
		},
		{
			name:   "empty value for a hidden field is no value",
			config: map[string]string{"name": "a", "audience": ""},
			creds:  map[string]string{"key": "secret"},
		},
		{
			name:   "field visible for several values",
			config: map[string]string{"name": "a", "auth": "token", "audience": "x"},
			creds:  map[string]string{"label": "l"},
		},
		{
			name:   "unknown controller value",
			config: map[string]string{"name": "a", "auth": "other"},
			want:   []destregistry.ValidationErrorDetail{{Field: "config.auth", Type: "enum"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newConditionalProvider(t)
			dest := &models.Destination{Type: "test", Config: tt.config, Credentials: tt.creds}
			assert.Equal(t, tt.want, validationErrors(t, p.Validate(context.Background(), dest)))
		})
	}
}

func TestBaseProvider_RemoveFieldOption(t *testing.T) {
	t.Parallel()

	t.Run("metadata drops the select and the option's fields", func(t *testing.T) {
		p := newConditionalProvider(t)
		p.RemoveFieldOption("auth", "token")

		got, err := json.Marshal(p.Metadata())
		require.NoError(t, err)

		want := metadata.ProviderMetadata{
			Type: "test",
			ConfigFields: []metadata.FieldSchema{
				{Key: "name", Type: "text", Required: true},
			},
			CredentialFields: []metadata.FieldSchema{
				{Key: "key", Type: "text", Required: true, Sensitive: true},
				{Key: "label", Type: "text"},
			},
		}
		wantJSON, err := json.Marshal(want)
		require.NoError(t, err)
		assert.JSONEq(t, string(wantJSON), string(got))
	})

	t.Run("source metadata is not modified", func(t *testing.T) {
		meta := conditionalMetadata()
		_, _ = meta.WithoutOption("auth", "token")
		assert.Equal(t, conditionalMetadata(), meta)
	})

	tests := []struct {
		name   string
		config map[string]string
		creds  map[string]string
		want   []destregistry.ValidationErrorDetail
	}{
		{
			name:   "no controller value",
			config: map[string]string{"name": "a"},
			creds:  map[string]string{"key": "secret"},
		},
		{
			name:   "remaining value",
			config: map[string]string{"name": "a", "auth": "key"},
			creds:  map[string]string{"key": "secret"},
		},
		{
			name:   "removed value",
			config: map[string]string{"name": "a", "auth": "token", "audience": "x"},
			creds:  map[string]string{"key": "secret"},
			want:   []destregistry.ValidationErrorDetail{{Field: "config.auth", Type: "enum"}},
		},
		{
			name:   "remaining fields stay required",
			config: map[string]string{"name": "a"},
			want:   []destregistry.ValidationErrorDetail{{Field: "credentials.key", Type: "required"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newConditionalProvider(t)
			p.RemoveFieldOption("auth", "token")
			dest := &models.Destination{Type: "test", Config: tt.config, Credentials: tt.creds}
			assert.Equal(t, tt.want, validationErrors(t, p.Validate(context.Background(), dest)))
		})
	}
}

func TestProviderMetadata_WithoutOptionKeepsSelectWithSeveralOptions(t *testing.T) {
	t.Parallel()
	meta := conditionalMetadata()
	meta.ConfigFields[1].Options = append(meta.ConfigFields[1].Options, metadata.FieldOption{Label: "Other", Value: "other"})

	out, fixed := meta.WithoutOption("auth", "token")
	assert.Empty(t, fixed)

	auth, ok := out.Field("auth")
	require.True(t, ok)
	assert.Equal(t, []metadata.FieldOption{{Label: "Key", Value: "key"}, {Label: "Other", Value: "other"}}, auth.Options)
	_, ok = out.Field("audience")
	assert.False(t, ok)
	label, ok := out.Field("label")
	require.True(t, ok)
	assert.Equal(t, &metadata.FieldCondition{Key: "auth", Values: []string{"key"}}, label.VisibleWhen)
}

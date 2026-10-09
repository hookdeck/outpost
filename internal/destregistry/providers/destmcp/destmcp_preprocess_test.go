package destmcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreprocess_Rotation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	const grace = 2 * time.Hour
	var (
		secretA = testSecret('a')
		secretB = testSecret('b')
		secretX = testSecret('x')
	)
	future := now.Add(time.Hour).Format(time.RFC3339)
	past := now.Add(-time.Second).Format(time.RFC3339)
	rotatedUntil := now.Add(grace).Format(time.RFC3339)

	tests := []struct {
		name   string
		stored map[string]string // nil: create
		sent   map[string]string
		want   map[string]string
	}{
		{
			name: "create keeps only the secret",
			sent: map[string]string{
				"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future, "rotate_secret": "true", "other": "x",
			},
			want: map[string]string{"secret": secretA},
		},
		{
			name: "create without credentials",
			sent: nil,
			want: map[string]string{},
		},
		{
			name:   "refresh with the same secret",
			stored: map[string]string{"secret": secretA},
			sent:   map[string]string{"secret": secretA},
			want:   map[string]string{"secret": secretA},
		},
		{
			name:   "refresh with a new secret rotates",
			stored: map[string]string{"secret": secretA},
			sent:   map[string]string{"secret": secretB},
			want:   map[string]string{"secret": secretB, "previous_secret": secretA, "previous_secret_invalid_at": rotatedUntil},
		},
		{
			name:   "same secret keeps a live previous pair",
			stored: map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future},
			sent:   map[string]string{"secret": secretA},
			want:   map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future},
		},
		{
			name:   "same secret drops an expired previous pair",
			stored: map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": past},
			sent:   map[string]string{"secret": secretA},
			want:   map[string]string{"secret": secretA},
		},
		{
			name:   "a previous pair expiring now is expired",
			stored: map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": now.Format(time.RFC3339)},
			sent:   map[string]string{"secret": secretA},
			want:   map[string]string{"secret": secretA},
		},
		{
			name:   "same secret drops an unreadable previous pair",
			stored: map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": "tomorrow"},
			sent:   map[string]string{"secret": secretA},
			want:   map[string]string{"secret": secretA},
		},
		{
			name:   "a second rotation within the grace replaces the previous secret",
			stored: map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future},
			sent:   map[string]string{"secret": secretB},
			want:   map[string]string{"secret": secretB, "previous_secret": secretA, "previous_secret_invalid_at": rotatedUntil},
		},
		{
			name:   "rotating back to the previous secret",
			stored: map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future},
			sent:   map[string]string{"secret": secretX},
			want:   map[string]string{"secret": secretX, "previous_secret": secretA, "previous_secret_invalid_at": rotatedUntil},
		},
		{
			name:   "a refresh without a secret keeps the stored credentials",
			stored: map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future},
			sent:   map[string]string{},
			want:   map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future},
		},
		{
			name:   "previous values sent by the caller are ignored",
			stored: map[string]string{"secret": secretA},
			sent:   map[string]string{"secret": secretA, "previous_secret": secretX, "previous_secret_invalid_at": future},
			want:   map[string]string{"secret": secretA},
		},
		{
			name:   "previous values sent with a new secret are ignored",
			stored: map[string]string{"secret": secretA},
			sent:   map[string]string{"secret": secretB, "previous_secret": secretX, "previous_secret_invalid_at": future},
			want:   map[string]string{"secret": secretB, "previous_secret": secretA, "previous_secret_invalid_at": rotatedUntil},
		},
		{
			name:   "unknown stored keys are dropped",
			stored: map[string]string{"secret": secretA, "legacy": "x"},
			sent:   map[string]string{"secret": secretA},
			want:   map[string]string{"secret": secretA},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := newProvider(t, func(c *destmcp.Config) { c.SecretRotationGrace = grace })
			destmcp.SetNow(p, func() time.Time { return now })

			var original *models.Destination
			if tt.stored != nil {
				original = &models.Destination{Type: destmcp.Type, Credentials: tt.stored}
			}
			dest := &models.Destination{Type: destmcp.Type, Credentials: tt.sent}
			require.NoError(t, p.Preprocess(dest, original, &destregistry.PreprocessDestinationOpts{Role: "admin"}))
			assert.Equal(t, models.Credentials(tt.want), dest.Credentials)
		})
	}
}

func TestPreprocess_DefaultGrace(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	p := newProvider(t)
	destmcp.SetNow(p, func() time.Time { return now })
	dest := &models.Destination{Type: destmcp.Type, Credentials: models.Credentials{"secret": testSecret('b')}}
	original := &models.Destination{Type: destmcp.Type, Credentials: models.Credentials{"secret": testSecret('a')}}
	require.NoError(t, p.Preprocess(dest, original, nil))
	assert.Equal(t, now.Add(24*time.Hour).Format(time.RFC3339), dest.Credentials["previous_secret_invalid_at"])
}

// A destination of another type with the same ID never lends its secret.
func TestPreprocess_IgnoresOtherTypes(t *testing.T) {
	t.Parallel()
	p := newProvider(t)
	dest := &models.Destination{Type: destmcp.Type, Credentials: models.Credentials{"secret": testSecret('b')}}
	webhook := &models.Destination{Type: "webhook", Credentials: models.Credentials{"secret": "webhook-secret"}}
	require.NoError(t, p.Preprocess(dest, webhook, nil))
	assert.Equal(t, models.Credentials{"secret": testSecret('b')}, dest.Credentials)
}

// The rotated destination validates, signs with both keys during the grace
// period and with the new one after it.
func TestPreprocess_RotatedDestinationValidates(t *testing.T) {
	t.Parallel()
	p := newProvider(t)
	original := newSubscription(t, p)
	refresh := newSubscription(t, p, func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = testSecret(9) })
	require.NoError(t, p.Preprocess(refresh, original, nil))
	require.NoError(t, p.Validate(t.Context(), refresh))
	assert.Equal(t, testSecret(1), refresh.Credentials[destmcp.CredentialPreviousSecret])
}

func TestObfuscateDestination(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	p := newProvider(t)
	destmcp.SetNow(p, func() time.Time { return now })

	t.Run("masks both secrets", func(t *testing.T) {
		d := newSubscription(t, p, func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = testSecret(2)
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = now.Add(time.Hour).Format(time.RFC3339)
		})
		before := map[string]string{}
		for k, v := range d.Credentials {
			before[k] = v
		}

		got := p.ObfuscateDestination(d)

		for _, key := range []string{destmcp.CredentialSecret, destmcp.CredentialPreviousSecret} {
			masked := got.Credentials[key]
			assert.Equal(t, "whse"+strings.Repeat("*", len(d.Credentials[key])-4), masked, key)
			assert.NotContains(t, masked, strings.TrimPrefix(d.Credentials[key], "whsec_")[:8])
		}
		assert.Equal(t, d.Credentials[destmcp.CredentialPreviousSecretInvalidAt], got.Credentials[destmcp.CredentialPreviousSecretInvalidAt])
		assert.Equal(t, d.Config, got.Config, "config is not sensitive")
		assert.Equal(t, before, map[string]string(d.Credentials), "the original is untouched")
	})

	t.Run("leaves out an expired previous secret", func(t *testing.T) {
		d := newSubscription(t, p, func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = testSecret(2)
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = now.Format(time.RFC3339)
		})
		got := p.ObfuscateDestination(d)
		assert.Equal(t, []string{destmcp.CredentialSecret}, keys(got.Credentials))
		assert.Contains(t, d.Credentials, destmcp.CredentialPreviousSecret)
	})

	t.Run("through the registry display", func(t *testing.T) {
		registry := newRegistry(t, destmcp.Config{Catalog: newCatalog(t)})
		d := newSubscription(t, mcpProvider(t, registry))
		display, err := registry.DisplayDestination(d)
		require.NoError(t, err)
		assert.NotEqual(t, d.Credentials[destmcp.CredentialSecret], display.Credentials[destmcp.CredentialSecret])
		assert.Equal(t, publicURL, display.Target)
	})
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

package destregistry_test

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type retryPolicyProvider struct {
	*mockProvider
	ok bool
}

func (p *retryPolicyProvider) RetryPolicy() (backoff.Backoff, int, bool) {
	return &backoff.ConstantBackoff{Interval: time.Minute}, 4, p.ok
}

func TestRegistry_RetryPolicy(t *testing.T) {
	t.Parallel()
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	plain, err := newMockProvider()
	require.NoError(t, err)
	require.NoError(t, registry.RegisterProvider("plain", plain))
	require.NoError(t, registry.RegisterProvider("own", &retryPolicyProvider{mockProvider: plain, ok: true}))
	require.NoError(t, registry.RegisterProvider("declined", &retryPolicyProvider{mockProvider: plain}))

	b, maxRetries, ok := registry.RetryPolicy("own")
	assert.True(t, ok)
	assert.Equal(t, 4, maxRetries)
	assert.Equal(t, time.Minute, b.Duration(0))

	for _, destType := range []string{"plain", "declined", "unknown"} {
		_, _, ok := registry.RetryPolicy(destType)
		assert.False(t, ok, destType)
	}
}

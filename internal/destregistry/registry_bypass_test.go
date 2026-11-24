package destregistry_test

import (
	"context"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bypassProvider struct {
	*mockProvider
	mu         sync.Mutex
	publishers []*mockPublisher
}

func (p *bypassProvider) BypassPublisherCache() bool { return true }

func (p *bypassProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	pub, err := p.mockProvider.CreatePublisher(ctx, dest)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.publishers = append(p.publishers, pub.(*mockPublisher))
	return pub, nil
}

func TestPublishEventPublisherCacheBypass(t *testing.T) {
	t.Parallel()
	logger := testutil.CreateTestLogger(t)

	t.Run("fresh publisher per attempt, closed afterwards", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{}, logger)
		base, err := newMockProvider()
		require.NoError(t, err)
		provider := &bypassProvider{mockProvider: base}
		require.NoError(t, registry.RegisterProvider("test", provider))

		dest := &models.Destination{ID: "dest_1", Type: "test"}
		for range 3 {
			attempt, err := registry.PublishEvent(context.Background(), dest, &models.Event{})
			require.NoError(t, err)
			assert.Equal(t, "success", attempt.Status)
		}
		require.Len(t, provider.publishers, 3)
		for _, pub := range provider.publishers {
			assert.True(t, pub.closed)
		}
	})

	t.Run("ResolvePublisher hands the caller its own publisher", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{}, logger)
		base, err := newMockProvider()
		require.NoError(t, err)
		provider := &bypassProvider{mockProvider: base}
		require.NoError(t, registry.RegisterProvider("test", provider))

		dest := &models.Destination{ID: "dest_1", Type: "test"}
		p1, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		p2, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		assert.NotSame(t, p1, p2)
		assert.False(t, p1.(*mockPublisher).closed)
	})

	t.Run("providers without it keep their cached publisher", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{}, logger)
		provider, err := newMockProvider()
		require.NoError(t, err)
		require.NoError(t, registry.RegisterProvider("test", provider))

		dest := &models.Destination{ID: "dest_1", Type: "test"}
		for range 3 {
			_, err := registry.PublishEvent(context.Background(), dest, &models.Event{})
			require.NoError(t, err)
		}
		assert.Equal(t, int32(1), provider.createCount)
	})
}

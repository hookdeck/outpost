package destregistry_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	createCount int32
}

type mockPublisher struct {
	id     int64
	closed bool
}

var mockPublisherID int64

func newMockPublisher() *mockPublisher {
	return &mockPublisher{
		id: atomic.AddInt64(&mockPublisherID, 1),
	}
}

func (p *mockProvider) Metadata() *metadata.ProviderMetadata                         { return nil }
func (p *mockProvider) Validate(ctx context.Context, dest *models.Destination) error { return nil }

func (p *mockProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	atomic.AddInt32(&p.createCount, 1)
	return newMockPublisher(), nil
}

func (p *mockPublisher) Publish(ctx context.Context, event *models.Event) error { return nil }
func (p *mockPublisher) Close() error {
	p.closed = true
	return nil
}

func TestRegistryConcurrentPublisherManagement(t *testing.T) {
	testutil.Race(t)

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	provider := &mockProvider{}
	registry.RegisterProvider("mock", provider)

	const numGoroutines = 100
	var wg sync.WaitGroup
	dest := &models.Destination{ID: "test", Type: "mock"}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pub, err := registry.ResolvePublisher(context.Background(), dest)
			require.NoError(t, err)
			require.NotNil(t, pub)
		}()
	}

	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&provider.createCount),
		"should create exactly one publisher despite concurrent access")
}

// Track where events are published by provider type and config
type publishTarget struct {
	providerType string
	config       string
}

var publishedEvents = make(map[publishTarget][]models.Event)

type mockPublisherWithConfig struct {
	providerType string
	config       string
}

func (p *mockPublisherWithConfig) Publish(ctx context.Context, event *models.Event) error {
	target := publishTarget{
		providerType: p.providerType,
		config:       p.config,
	}
	publishedEvents[target] = append(publishedEvents[target], *event)
	return nil
}

func (p *mockPublisherWithConfig) Close() error { return nil }

type mockProviderWithConfig struct {
	providerType string
}

func (p *mockProviderWithConfig) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	return &mockPublisherWithConfig{
		providerType: p.providerType,
		config:       dest.Config["id"], // Use generic config ID instead of URL
	}, nil
}

func (p *mockProviderWithConfig) Metadata() *metadata.ProviderMetadata { return nil }
func (p *mockProviderWithConfig) Validate(ctx context.Context, dest *models.Destination) error {
	return nil
}

func TestDestinationChanges(t *testing.T) {
	t.Parallel()
	t.Run("config change", func(t *testing.T) {
		publishedEvents = make(map[publishTarget][]models.Event)
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		provider := &mockProviderWithConfig{providerType: "mock1"}
		registry.RegisterProvider("mock1", provider)

		// Create initial destination
		dest := &models.Destination{
			ID:   "test-dest",
			Type: "mock1",
			Config: map[string]string{
				"id": "config1",
			},
		}

		// Publish first event
		event1 := &models.Event{Data: map[string]interface{}{"msg": "first"}}
		err := registry.PublishEvent(context.Background(), dest, event1)
		require.NoError(t, err)

		// Update destination with new config
		destUpdated := &models.Destination{
			ID:   "test-dest", // Same ID
			Type: "mock1",     // Same type
			Config: map[string]string{
				"id": "config2", // Different config
			},
		}

		// Publish second event - should go to new config
		event2 := &models.Event{Data: map[string]interface{}{"msg": "second"}}
		err = registry.PublishEvent(context.Background(), destUpdated, event2)
		require.NoError(t, err)

		firstTarget := publishTarget{providerType: "mock1", config: "config1"}
		secondTarget := publishTarget{providerType: "mock1", config: "config2"}

		// These assertions will fail with current implementation
		firstEvents := publishedEvents[firstTarget]
		assert.Len(t, firstEvents, 1, "only first event should go to config1")
		if len(firstEvents) > 0 {
			assert.Equal(t, "first", firstEvents[0].Data["msg"])
		}

		secondEvents := publishedEvents[secondTarget]
		assert.Len(t, secondEvents, 1, "second event should go to config2")
		if len(secondEvents) > 0 {
			assert.Equal(t, "second", secondEvents[0].Data["msg"])
		}
	})

	t.Run("type change", func(t *testing.T) {
		publishedEvents = make(map[publishTarget][]models.Event)
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		provider1 := &mockProviderWithConfig{providerType: "mock1"}
		provider2 := &mockProviderWithConfig{providerType: "mock2"}
		registry.RegisterProvider("mock1", provider1)
		registry.RegisterProvider("mock2", provider2)

		// Create initial destination
		dest := &models.Destination{
			ID:   "test-dest",
			Type: "mock1",
			Config: map[string]string{
				"id": "config1",
			},
		}

		// Publish first event
		event1 := &models.Event{Data: map[string]interface{}{"msg": "first"}}
		err := registry.PublishEvent(context.Background(), dest, event1)
		require.NoError(t, err)

		// Update destination type
		destUpdated := &models.Destination{
			ID:   "test-dest", // Same ID
			Type: "mock2",     // Different type
			Config: map[string]string{
				"id": "config1", // Same config
			},
		}

		// Publish second event - should use different provider
		event2 := &models.Event{Data: map[string]interface{}{"msg": "second"}}
		err = registry.PublishEvent(context.Background(), destUpdated, event2)
		require.NoError(t, err)

		firstTarget := publishTarget{providerType: "mock1", config: "config1"}
		secondTarget := publishTarget{providerType: "mock2", config: "config1"}

		// These assertions will fail with current implementation
		firstEvents := publishedEvents[firstTarget]
		assert.Len(t, firstEvents, 1, "only first event should go through mock1")
		if len(firstEvents) > 0 {
			assert.Equal(t, "first", firstEvents[0].Data["msg"])
		}

		secondEvents := publishedEvents[secondTarget]
		assert.Len(t, secondEvents, 1, "second event should go through mock2")
		if len(secondEvents) > 0 {
			assert.Equal(t, "second", secondEvents[0].Data["msg"])
		}
	})
}

func TestPublisherExpiration(t *testing.T) {
	t.Parallel()

	t.Run("basic_ttl", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{
			PublisherTTL: 100 * time.Millisecond,
		}, testutil.CreateTestLogger(t))
		provider := &mockProvider{}
		registry.RegisterProvider("mock", provider)

		dest := &models.Destination{ID: "test", Type: "mock"}

		// Get initial publisher
		p1, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		id1 := p1.(*mockPublisher).id

		// Wait > TTL
		time.Sleep(110 * time.Millisecond)

		// Get new publisher - should be different since p1 expired
		p2, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		assert.NotEqual(t, id1, p2.(*mockPublisher).id)
	})

	t.Run("refresh_extends_ttl", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{
			PublisherTTL: 100 * time.Millisecond,
		}, testutil.CreateTestLogger(t))
		provider := &mockProvider{}
		registry.RegisterProvider("mock", provider)

		dest := &models.Destination{ID: "test", Type: "mock"}

		// Get initial publisher
		p1, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		id1 := p1.(*mockPublisher).id

		// Wait 90ms (almost expired)
		time.Sleep(90 * time.Millisecond)

		// Access refreshes TTL
		p2, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		assert.Equal(t, id1, p2.(*mockPublisher).id)

		// Wait 90ms (within refreshed TTL)
		time.Sleep(90 * time.Millisecond)

		// Still alive since last access refreshed TTL
		p3, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		assert.Equal(t, id1, p3.(*mockPublisher).id)

		// Wait > TTL for final expiration
		time.Sleep(110 * time.Millisecond)

		// Should get new publisher
		p4, err := registry.ResolvePublisher(context.Background(), dest)
		require.NoError(t, err)
		assert.NotEqual(t, id1, p4.(*mockPublisher).id)
	})
}

func TestPublisherCapacity(t *testing.T) {
	t.Parallel()

	t.Run("basic eviction without access", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{
			PublisherCacheSize: 2,         // Size of 2 for testing
			PublisherTTL:       time.Hour, // Long TTL to ensure expiration doesn't interfere
		}, testutil.CreateTestLogger(t))
		provider := &mockProvider{}
		registry.RegisterProvider("mock", provider)

		// Create 3 destinations with different IDs
		dests := []*models.Destination{
			{ID: "test1", Type: "mock"},
			{ID: "test2", Type: "mock"},
			{ID: "test3", Type: "mock"},
		}

		// Get publishers for first two destinations
		p1, err := registry.ResolvePublisher(context.Background(), dests[0])
		require.NoError(t, err)
		id1 := p1.(*mockPublisher).id
		// Cache: [p1]

		p2, err := registry.ResolvePublisher(context.Background(), dests[1])
		require.NoError(t, err)
		id2 := p2.(*mockPublisher).id
		// Cache: [p2, p1]

		// Get publisher for third destination - should evict p1
		_, err = registry.ResolvePublisher(context.Background(), dests[2])
		require.NoError(t, err)
		// Cache: [p3, p2], p1 evicted

		// Verify p2 is still cached
		p2Again, err := registry.ResolvePublisher(context.Background(), dests[1])
		require.NoError(t, err)
		assert.Equal(t, id2, p2Again.(*mockPublisher).id, "Expected p2 to still be cached")

		// Try to get first publisher again - should be new instance
		p1Again, err := registry.ResolvePublisher(context.Background(), dests[0])
		require.NoError(t, err)
		assert.NotEqual(t, id1, p1Again.(*mockPublisher).id, "Expected p1 to be recreated")
	})

	t.Run("access refreshes cache order", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{
			PublisherCacheSize: 2,
			PublisherTTL:       time.Hour,
		}, testutil.CreateTestLogger(t))
		provider := &mockProvider{}
		registry.RegisterProvider("mock", provider)

		dests := []*models.Destination{
			{ID: "test1", Type: "mock"},
			{ID: "test2", Type: "mock"},
			{ID: "test3", Type: "mock"},
		}

		// Get first two publishers
		p1, err := registry.ResolvePublisher(context.Background(), dests[0])
		require.NoError(t, err)
		id1 := p1.(*mockPublisher).id
		// Cache: [p1]

		p2, err := registry.ResolvePublisher(context.Background(), dests[1])
		require.NoError(t, err)
		id2 := p2.(*mockPublisher).id
		// Cache: [p2, p1]

		// Access p1 to make it most recently used
		p1Again, err := registry.ResolvePublisher(context.Background(), dests[0])
		require.NoError(t, err)
		assert.Equal(t, id1, p1Again.(*mockPublisher).id, "Expected same p1 instance")
		// Cache: [p1, p2]

		// Add p3 - should evict p2 since it's now least recently used
		_, err = registry.ResolvePublisher(context.Background(), dests[2])
		require.NoError(t, err)
		// Cache: [p3, p1], p2 evicted

		// Verify p1 is still cached
		p1Again, err = registry.ResolvePublisher(context.Background(), dests[0])
		require.NoError(t, err)
		assert.Equal(t, id1, p1Again.(*mockPublisher).id, "Expected p1 to still be cached")

		// Verify p2 was evicted
		p2Again, err := registry.ResolvePublisher(context.Background(), dests[1])
		require.NoError(t, err)
		assert.NotEqual(t, id2, p2Again.(*mockPublisher).id, "Expected p2 to be recreated")
	})
}

func TestPublisherEviction(t *testing.T) {
	t.Parallel()
	registry := destregistry.NewRegistry(&destregistry.Config{
		PublisherCacheSize: 1,         // Smallest possible size
		PublisherTTL:       time.Hour, // Long TTL to ensure expiration doesn't interfere
	}, testutil.CreateTestLogger(t))
	provider := &mockProvider{}
	registry.RegisterProvider("mock", provider)

	// Create 2 destinations with different IDs
	dests := []*models.Destination{
		{ID: "test1", Type: "mock"},
		{ID: "test2", Type: "mock"},
	}

	// Get first publisher
	p1, err := registry.ResolvePublisher(context.Background(), dests[0])
	require.NoError(t, err)
	mp1 := p1.(*mockPublisher)
	// Cache: [p1]

	// Get second publisher - should evict p1
	_, err = registry.ResolvePublisher(context.Background(), dests[1])
	require.NoError(t, err)
	// Cache: [p2], p1 evicted

	assert.True(t, mp1.closed, "Expected evicted publisher to be closed")
}

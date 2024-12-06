package destregistry_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

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

type mockPublisher struct{}

func (p *mockProvider) Metadata() *metadata.ProviderMetadata                         { return nil }
func (p *mockProvider) Validate(ctx context.Context, dest *models.Destination) error { return nil }

func (p *mockProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	atomic.AddInt32(&p.createCount, 1)
	return &mockPublisher{}, nil
}

func (p *mockPublisher) Publish(ctx context.Context, event *models.Event) error { return nil }
func (p *mockPublisher) Close() error                                           { return nil }

func TestRegistryConcurrentPublisherManagement(t *testing.T) {
	testutil.Race(t)

	registry := destregistry.NewRegistry(&destregistry.Config{})
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
	t.Run("config change", func(t *testing.T) {
		publishedEvents = make(map[publishTarget][]models.Event)
		registry := destregistry.NewRegistry(&destregistry.Config{})
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
		registry := destregistry.NewRegistry(&destregistry.Config{})
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

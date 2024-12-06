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

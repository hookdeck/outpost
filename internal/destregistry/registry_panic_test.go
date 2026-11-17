package destregistry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type panicPublisher struct {
	mu           sync.Mutex
	panicPublish bool
	panicClose   bool
	closes       int
}

func (p *panicPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if p.panicPublish {
		panic("publish blew up")
	}
	return &destregistry.Delivery{Status: "success", Code: "OK"}, nil
}

func (p *panicPublisher) Close() error {
	p.mu.Lock()
	p.closes++
	p.mu.Unlock()
	if p.panicClose {
		panic("close blew up")
	}
	return nil
}

func (p *panicPublisher) closeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closes
}

// panicProvider hands out panicPublishers. The first publisher it creates
// panics in Publish when firstPanicsPub is set, in Close when
// firstPanicsClose is set; later ones behave.
type panicProvider struct {
	*mockProvider
	bypass           bool
	panicCreate      bool
	firstPanicsPub   bool
	firstPanicsClose bool
	mu               sync.Mutex
	created          []*panicPublisher
}

func (p *panicProvider) BypassPublisherCache() bool { return p.bypass }

func (p *panicProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	if p.panicCreate {
		panic("create blew up")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	first := len(p.created) == 0
	pub := &panicPublisher{
		panicPublish: first && p.firstPanicsPub,
		panicClose:   first && p.firstPanicsClose,
	}
	p.created = append(p.created, pub)
	return pub, nil
}

func (p *panicProvider) publishers() []*panicPublisher {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*panicPublisher(nil), p.created...)
}

func newPanicRegistry(t *testing.T, cfg *destregistry.Config, provider *panicProvider) destregistry.Registry {
	t.Helper()
	base, err := newMockProvider()
	require.NoError(t, err)
	provider.mockProvider = base
	registry := destregistry.NewRegistry(cfg, testutil.CreateTestLogger(t))
	t.Cleanup(func() { destregistry.CloseForTest(registry) })
	require.NoError(t, registry.RegisterProvider("test", provider))
	return registry
}

// requirePanicError checks what PublishEvent returns for a recovered panic:
// no attempt and a plain error, which the delivery handler nacks.
func requirePanicError(t *testing.T, attempt *models.Attempt, err error, dest *models.Destination) {
	t.Helper()

	assert.Nil(t, attempt, "a panic records no attempt")
	require.ErrorContains(t, err, "panic publishing to "+dest.Type+" destination")
	var pubErr *destregistry.ErrDestinationPublishAttempt
	assert.False(t, errors.As(err, &pubErr), "an attempt error would ack the message")
}

func TestPublishEventRecoversFromPanics(t *testing.T) {
	t.Parallel()

	dest := &models.Destination{ID: "dest_1", Type: "test"}
	event := &models.Event{ID: "evt_1"}

	t.Run("panic in Publish evicts and closes the cached publisher once", func(t *testing.T) {
		t.Parallel()
		provider := &panicProvider{firstPanicsPub: true}
		registry := newPanicRegistry(t, &destregistry.Config{}, provider)

		var attempt *models.Attempt
		var err error
		require.NotPanics(t, func() {
			attempt, err = registry.PublishEvent(context.Background(), dest, event)
		})
		requirePanicError(t, attempt, err, dest)

		pubs := provider.publishers()
		require.Len(t, pubs, 1)
		assert.Equal(t, 1, pubs[0].closeCount(), "closed exactly once")

		attempt, err = registry.PublishEvent(context.Background(), dest, event)
		require.NoError(t, err)
		assert.Equal(t, "success", attempt.Status)
		pubs = provider.publishers()
		require.Len(t, pubs, 2, "the next attempt builds a fresh publisher")
		assert.Equal(t, 0, pubs[1].closeCount())
		assert.Equal(t, 1, pubs[0].closeCount())
	})

	t.Run("panic in Publish and in Close is contained", func(t *testing.T) {
		t.Parallel()
		provider := &panicProvider{firstPanicsPub: true, firstPanicsClose: true}
		registry := newPanicRegistry(t, &destregistry.Config{}, provider)

		var attempt *models.Attempt
		var err error
		require.NotPanics(t, func() {
			attempt, err = registry.PublishEvent(context.Background(), dest, event)
		})
		requirePanicError(t, attempt, err, dest)
		assert.Equal(t, 1, provider.publishers()[0].closeCount())
	})

	t.Run("panic in CreatePublisher", func(t *testing.T) {
		t.Parallel()
		provider := &panicProvider{panicCreate: true}
		registry := newPanicRegistry(t, &destregistry.Config{}, provider)

		var attempt *models.Attempt
		var err error
		require.NotPanics(t, func() {
			attempt, err = registry.PublishEvent(context.Background(), dest, event)
		})
		requirePanicError(t, attempt, err, dest)
	})

	t.Run("cache bypass: panic in Publish closes the publisher once", func(t *testing.T) {
		t.Parallel()
		provider := &panicProvider{bypass: true, firstPanicsPub: true}
		registry := newPanicRegistry(t, &destregistry.Config{}, provider)

		var attempt *models.Attempt
		var err error
		require.NotPanics(t, func() {
			attempt, err = registry.PublishEvent(context.Background(), dest, event)
		})
		requirePanicError(t, attempt, err, dest)
		pubs := provider.publishers()
		require.Len(t, pubs, 1)
		assert.Equal(t, 1, pubs[0].closeCount())

		attempt, err = registry.PublishEvent(context.Background(), dest, event)
		require.NoError(t, err)
		assert.Equal(t, "success", attempt.Status)
	})

	t.Run("cache bypass: panic in Close keeps the successful attempt", func(t *testing.T) {
		t.Parallel()
		provider := &panicProvider{bypass: true, firstPanicsClose: true}
		registry := newPanicRegistry(t, &destregistry.Config{}, provider)

		var attempt *models.Attempt
		var err error
		require.NotPanics(t, func() {
			attempt, err = registry.PublishEvent(context.Background(), dest, event)
		})
		require.NoError(t, err)
		require.NotNil(t, attempt)
		assert.Equal(t, "success", attempt.Status)
	})
}

func TestPublisherCacheEvictionContainsClosePanic(t *testing.T) {
	t.Parallel()

	provider := &panicProvider{firstPanicsClose: true}
	registry := newPanicRegistry(t, &destregistry.Config{PublisherCacheSize: 1}, provider)

	first := &models.Destination{ID: "dest_1", Type: "test"}
	second := &models.Destination{ID: "dest_2", Type: "test"}

	_, err := registry.ResolvePublisher(context.Background(), first)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, err = registry.ResolvePublisher(context.Background(), second)
	})
	require.NoError(t, err)
	assert.Equal(t, 1, provider.publishers()[0].closeCount(), "the evicted publisher was closed")

	attempt, err := registry.PublishEvent(context.Background(), second, &models.Event{ID: "evt_1"})
	require.NoError(t, err)
	assert.Equal(t, "success", attempt.Status, "the cache still works after the panic")
}

// gatedPublisher blocks each Publish until released, then panics.
type gatedPublisher struct {
	panicPublisher
	entered chan struct{}
	release chan struct{}
}

func (p *gatedPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	p.entered <- struct{}{}
	<-p.release
	panic("publish blew up")
}

// gatedProvider hands out one gatedPublisher, then healthy publishers.
type gatedProvider struct {
	*mockProvider
	mu      sync.Mutex
	first   *gatedPublisher
	healthy []*panicPublisher

	handedOutFirst bool
}

func newGatedProvider(base *mockProvider) *gatedProvider {
	return &gatedProvider{
		mockProvider: base,
		first:        &gatedPublisher{entered: make(chan struct{}, 2), release: make(chan struct{})},
	}
}

func (p *gatedProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.handedOutFirst {
		p.handedOutFirst = true
		return p.first, nil
	}
	pub := &panicPublisher{}
	p.healthy = append(p.healthy, pub)
	return pub, nil
}

func TestPublishEventLatePanicKeepsReplacementPublisher(t *testing.T) {
	t.Parallel()

	base, err := newMockProvider()
	require.NoError(t, err)
	provider := newGatedProvider(base)
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	t.Cleanup(func() { destregistry.CloseForTest(registry) })
	require.NoError(t, registry.RegisterProvider("test", provider))

	dest := &models.Destination{ID: "dest_1", Type: "test"}
	event := &models.Event{ID: "evt_1"}

	// Two publishes in flight on the same cached publisher. The second starts
	// once the first is inside Publish, so it finds the publisher in the cache.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = registry.PublishEvent(context.Background(), dest, event)
		}()
		<-provider.first.entered
	}

	// The first panic drops the gated publisher from the cache.
	provider.first.release <- struct{}{}
	require.Eventually(t, func() bool { return provider.first.closeCount() == 1 }, 5*time.Second, 5*time.Millisecond)

	attempt, err := registry.PublishEvent(context.Background(), dest, event)
	require.NoError(t, err)
	require.Equal(t, "success", attempt.Status)

	// The second panic comes from the old publisher and must leave its
	// replacement in the cache.
	provider.first.release <- struct{}{}
	wg.Wait()

	attempt, err = registry.PublishEvent(context.Background(), dest, event)
	require.NoError(t, err)
	require.Equal(t, "success", attempt.Status)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.Len(t, provider.healthy, 1, "the replacement is reused, not rebuilt")
	assert.Equal(t, 0, provider.healthy[0].closeCount())
	assert.Equal(t, 1, provider.first.closeCount())
}

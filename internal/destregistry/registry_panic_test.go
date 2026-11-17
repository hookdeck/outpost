package destregistry_test

import (
	"context"
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
// panics in Publish when firstPanicsPublish is set, in Close when
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

func requirePanicAttempt(t *testing.T, attempt *models.Attempt, err error, dest *models.Destination, event *models.Event) {
	t.Helper()

	require.NotNil(t, attempt, "a nil attempt would nack the message")
	assert.NotEmpty(t, attempt.ID)
	assert.Equal(t, dest.ID, attempt.DestinationID)
	assert.Equal(t, dest.Type, attempt.DestinationType)
	assert.Equal(t, event.ID, attempt.EventID)
	assert.False(t, attempt.Time.IsZero(), "retry scheduling reads the attempt time")
	assert.WithinDuration(t, time.Now(), attempt.Time, time.Minute)
	assert.Equal(t, "failed", attempt.Status)
	assert.Equal(t, "ERR", attempt.Code)
	respErr, _ := attempt.ResponseData["error"].(string)
	assert.NotEmpty(t, respErr)
	assert.NotContains(t, respErr, "blew up", "panic text stays out of the tenant-facing response")
	assert.NotContains(t, respErr, "goroutine")

	var pubErr *destregistry.ErrDestinationPublishAttempt
	require.ErrorAs(t, err, &pubErr)
	assert.Equal(t, dest.Type, pubErr.Provider)
	assert.False(t, pubErr.NonRetryable)
	assert.False(t, pubErr.SkipFailureCount)
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
		requirePanicAttempt(t, attempt, err, dest, event)

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
		requirePanicAttempt(t, attempt, err, dest, event)
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
		requirePanicAttempt(t, attempt, err, dest, event)
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
		requirePanicAttempt(t, attempt, err, dest, event)
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

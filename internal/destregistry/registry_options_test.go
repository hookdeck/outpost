package destregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionsProvider is a mockProvider that implements the optional provider
// interfaces and records every publisher it creates.
type optionsProvider struct {
	*mockProvider
	timeout     time.Duration
	bypass      bool
	validateErr error

	mu         sync.Mutex
	publishers []*mockPublisher
}

var (
	_ destregistry.DeliveryTimeouter      = (*optionsProvider)(nil)
	_ destregistry.PublisherCacheBypasser = (*optionsProvider)(nil)
)

func newOptionsProvider(t *testing.T) *optionsProvider {
	t.Helper()
	base, err := newMockProvider()
	require.NoError(t, err)
	return &optionsProvider{mockProvider: base}
}

func (p *optionsProvider) DeliveryTimeout() time.Duration { return p.timeout }
func (p *optionsProvider) BypassPublisherCache() bool     { return p.bypass }

func (p *optionsProvider) Validate(ctx context.Context, dest *models.Destination) error {
	return p.validateErr
}

func (p *optionsProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	pub, err := p.mockProvider.CreatePublisher(ctx, dest)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.publishers = append(p.publishers, pub.(*mockPublisher))
	p.mu.Unlock()
	return pub, nil
}

func (p *optionsProvider) created() []*mockPublisher {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*mockPublisher(nil), p.publishers...)
}

func TestPublishEvent_ProviderDeliveryTimeout(t *testing.T) {
	t.Parallel()

	t.Run("a longer provider timeout lets a slow attempt finish", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{DeliveryTimeout: 50 * time.Millisecond}, testutil.CreateTestLogger(t))
		provider := newOptionsProvider(t)
		provider.timeout = 2 * time.Second
		provider.publishDelay = 150 * time.Millisecond
		require.NoError(t, registry.RegisterProvider("opt", provider))

		attempt, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "d", Type: "opt"}, &models.Event{ID: "e"})
		require.NoError(t, err)
		assert.Equal(t, "success", attempt.Status)
	})

	t.Run("a shorter provider timeout bounds the attempt and is reported", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{DeliveryTimeout: 5 * time.Second}, testutil.CreateTestLogger(t))
		provider := newOptionsProvider(t)
		provider.timeout = 50 * time.Millisecond
		provider.publishDelay = time.Second
		require.NoError(t, registry.RegisterProvider("opt", provider))

		start := time.Now()
		_, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "d", Type: "opt"}, &models.Event{ID: "e"})
		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second)

		var pubErr *destregistry.ErrDestinationPublishAttempt
		require.ErrorAs(t, err, &pubErr)
		assert.Equal(t, "timeout", pubErr.Data["error"])
		assert.Equal(t, "50ms", pubErr.Data["timeout"], "timeout data shows the effective (provider) value")
	})

	t.Run("a non-positive provider timeout falls back to the global one", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{DeliveryTimeout: 50 * time.Millisecond}, testutil.CreateTestLogger(t))
		provider := newOptionsProvider(t)
		provider.timeout = 0
		provider.publishDelay = time.Second
		require.NoError(t, registry.RegisterProvider("opt", provider))

		_, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "d", Type: "opt"}, &models.Event{ID: "e"})
		var pubErr *destregistry.ErrDestinationPublishAttempt
		require.ErrorAs(t, err, &pubErr)
		assert.Equal(t, "50ms", pubErr.Data["timeout"])
	})
}

func TestPublishEvent_PublisherCacheBypass(t *testing.T) {
	t.Parallel()

	registry := destregistry.NewRegistry(&destregistry.Config{
		PublisherCacheSize: 1,
		PublisherTTL:       time.Hour,
	}, testutil.CreateTestLogger(t))

	cachedProvider := newOptionsProvider(t)
	require.NoError(t, registry.RegisterProvider("cached", cachedProvider))
	bypassProvider := newOptionsProvider(t)
	bypassProvider.bypass = true
	require.NoError(t, registry.RegisterProvider("bypass", bypassProvider))

	cachedDest := &models.Destination{ID: "cached-1", Type: "cached"}
	bypassDest := &models.Destination{ID: "bypass-1", Type: "bypass"}

	cached, err := registry.ResolvePublisher(context.Background(), cachedDest)
	require.NoError(t, err)

	for range 3 {
		attempt, err := registry.PublishEvent(context.Background(), bypassDest, &models.Event{ID: "e"})
		require.NoError(t, err)
		assert.Equal(t, "success", attempt.Status)
	}

	pubs := bypassProvider.created()
	require.Len(t, pubs, 3, "a bypassing provider gets a fresh publisher per attempt")
	for i, p := range pubs {
		assert.True(t, p.closed.Load(), "publisher %d is closed after its attempt", i)
	}

	// The single cache slot still holds the cached provider's publisher: the
	// bypassed publishers never entered (or evicted from) the cache.
	again, err := registry.ResolvePublisher(context.Background(), cachedDest)
	require.NoError(t, err)
	assert.Same(t, cached, again)
	assert.False(t, cached.(*mockPublisher).closed.Load())

	// ResolvePublisher hands out an uncached publisher the caller owns.
	p1, err := registry.ResolvePublisher(context.Background(), bypassDest)
	require.NoError(t, err)
	p2, err := registry.ResolvePublisher(context.Background(), bypassDest)
	require.NoError(t, err)
	assert.NotSame(t, p1, p2)
	assert.False(t, p1.(*mockPublisher).closed.Load(), "the registry does not close a publisher it handed out")
}

func TestPublishEvent_BypassedPublisherClosedOnFailure(t *testing.T) {
	t.Parallel()

	registry := destregistry.NewRegistry(&destregistry.Config{DeliveryTimeout: time.Second}, testutil.CreateTestLogger(t))
	provider := newOptionsProvider(t)
	provider.bypass = true
	provider.failDelivery = &destregistry.Delivery{Status: "failed", Code: "500"}
	provider.mockError = destregistry.NewErrDestinationPublishAttempt(errors.New("boom"), "bypass", nil)
	require.NoError(t, registry.RegisterProvider("bypass", provider))

	_, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "d", Type: "bypass"}, &models.Event{ID: "e"})
	require.Error(t, err)
	pubs := provider.created()
	require.Len(t, pubs, 1)
	assert.True(t, pubs[0].closed.Load())
}

// slowClosePublisher's Close blocks until released, like a BasePublisher
// waiting on an in-flight publish.
type slowClosePublisher struct {
	release <-chan struct{}
	closing chan struct{}
}

func (p *slowClosePublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	return &destregistry.Delivery{Status: "success", Code: "OK"}, nil
}

func (p *slowClosePublisher) Close() error {
	close(p.closing)
	<-p.release
	return nil
}

type slowCloseProvider struct {
	*mockProvider
	release chan struct{}
	mu      sync.Mutex
	pubs    []*slowClosePublisher
}

func (p *slowCloseProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	pub := &slowClosePublisher{release: p.release, closing: make(chan struct{})}
	p.mu.Lock()
	p.pubs = append(p.pubs, pub)
	p.mu.Unlock()
	return pub, nil
}

func TestPublisherEviction_SlowCloseDoesNotBlock(t *testing.T) {
	t.Parallel()

	registry := destregistry.NewRegistry(&destregistry.Config{
		PublisherCacheSize: 1,
		PublisherTTL:       time.Hour,
	}, testutil.CreateTestLogger(t))
	base, err := newMockProvider()
	require.NoError(t, err)
	provider := &slowCloseProvider{mockProvider: base, release: make(chan struct{})}
	defer close(provider.release)
	require.NoError(t, registry.RegisterProvider("slow", provider))

	_, err = registry.ResolvePublisher(context.Background(), &models.Destination{ID: "a", Type: "slow"})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Evicts "a", whose Close blocks until release.
		_, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "b", Type: "slow"}, &models.Event{ID: "e"})
		assert.NoError(t, err)
		// The cache stays usable while "a" is still closing.
		_, err = registry.ResolvePublisher(context.Background(), &models.Destination{ID: "c", Type: "slow"})
		assert.NoError(t, err)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("eviction blocked on a slow publisher Close")
	}

	provider.mu.Lock()
	first := provider.pubs[0]
	provider.mu.Unlock()
	select {
	case <-first.closing:
	case <-time.After(2 * time.Second):
		t.Fatal("the evicted publisher was never closed")
	}
}

func TestPublishEvent_NonRetryable(t *testing.T) {
	t.Parallel()

	t.Run("flag passes through on a provider attempt error", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{DeliveryTimeout: time.Second}, testutil.CreateTestLogger(t))
		provider := newOptionsProvider(t)
		provider.failDelivery = &destregistry.Delivery{Status: "failed", Code: "410"}
		provider.mockError = &destregistry.ErrDestinationPublishAttempt{
			Err: errors.New("gone"), Provider: "opt", NonRetryable: true,
		}
		require.NoError(t, registry.RegisterProvider("opt", provider))

		attempt, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "d", Type: "opt"}, &models.Event{ID: "e"})
		require.Error(t, err)
		require.NotNil(t, attempt)
		assert.Equal(t, "410", attempt.Code)
		assert.True(t, destregistry.IsNonRetryable(err))
	})

	t.Run("flag survives the timeout rewrite", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{DeliveryTimeout: 20 * time.Millisecond}, testutil.CreateTestLogger(t))
		provider := newOptionsProvider(t)
		provider.publishDelay = time.Second
		provider.mockError = &destregistry.ErrDestinationPublishAttempt{
			Err: context.DeadlineExceeded, Provider: "opt", NonRetryable: true,
		}
		require.NoError(t, registry.RegisterProvider("opt", provider))

		_, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "d", Type: "opt"}, &models.Event{ID: "e"})
		var pubErr *destregistry.ErrDestinationPublishAttempt
		require.ErrorAs(t, err, &pubErr)
		assert.Equal(t, "timeout", pubErr.Data["error"])
		assert.True(t, pubErr.NonRetryable)
	})

	t.Run("plain timeouts stay retryable", func(t *testing.T) {
		t.Parallel()
		registry := destregistry.NewRegistry(&destregistry.Config{DeliveryTimeout: 20 * time.Millisecond}, testutil.CreateTestLogger(t))
		provider := newOptionsProvider(t)
		provider.publishDelay = time.Second
		require.NoError(t, registry.RegisterProvider("opt", provider))

		_, err := registry.PublishEvent(context.Background(), &models.Destination{ID: "d", Type: "opt"}, &models.Event{ID: "e"})
		require.Error(t, err)
		assert.False(t, destregistry.IsNonRetryable(err))
	})
}

func TestIsNonRetryable(t *testing.T) {
	t.Parallel()
	nonRetryable := &destregistry.ErrDestinationPublishAttempt{Err: errors.New("gone"), NonRetryable: true}
	retryable := &destregistry.ErrDestinationPublishAttempt{Err: errors.New("503")}

	assert.True(t, destregistry.IsNonRetryable(nonRetryable))
	assert.True(t, destregistry.IsNonRetryable(fmt.Errorf("wrapped: %w", nonRetryable)))
	assert.True(t, destregistry.IsNonRetryable(errors.Join(errors.New("other"), nonRetryable)))
	assert.False(t, destregistry.IsNonRetryable(retryable))
	assert.False(t, destregistry.IsNonRetryable(errors.New("plain")))
	assert.False(t, destregistry.IsNonRetryable(nil))
}

// errProtocol stands in for a provider-specific error carried as Cause.
type errProtocol struct{ kind string }

func (e *errProtocol) Error() string { return "protocol error: " + e.kind }

func TestErrDestinationValidation_Cause(t *testing.T) {
	t.Parallel()

	cause := &errProtocol{kind: "not_found"}
	valErr := &destregistry.ErrDestinationValidation{
		Errors: []destregistry.ValidationErrorDetail{{Field: "config.event", Type: "not_found"}},
		Cause:  cause,
	}

	var got *errProtocol
	require.ErrorAs(t, valErr, &got, "Cause is reachable through Unwrap")
	assert.Same(t, cause, got)
	assert.ErrorIs(t, fmt.Errorf("ctx: %w", valErr), cause)

	assert.Equal(t, "validation failed", valErr.Error(), "Error() never includes the cause")
	b, err := json.Marshal(valErr)
	require.NoError(t, err)
	assert.JSONEq(t, `{"errors":[{"field":"config.event","type":"not_found"}]}`, string(b))

	assert.NoError(t, (&destregistry.ErrDestinationValidation{}).Unwrap())
}

func TestValidateDestination_PassesValidationErrorThrough(t *testing.T) {
	t.Parallel()

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	provider := newOptionsProvider(t)
	require.NoError(t, registry.RegisterProvider("opt", provider))
	dest := &models.Destination{ID: "d", Type: "opt"}

	t.Run("same pointer, cause intact", func(t *testing.T) {
		valErr := &destregistry.ErrDestinationValidation{
			Errors: []destregistry.ValidationErrorDetail{{Field: "config.url", Type: "address_not_allowed"}},
			Cause:  &errProtocol{kind: "invalid_params"},
		}
		provider.validateErr = valErr
		err := registry.ValidateDestination(context.Background(), dest)
		require.Error(t, err)
		got, ok := err.(*destregistry.ErrDestinationValidation)
		require.True(t, ok, "returned unwrapped as *ErrDestinationValidation")
		assert.Same(t, valErr, got)
		var cause *errProtocol
		require.ErrorAs(t, err, &cause)
		assert.Equal(t, "invalid_params", cause.kind)
	})

	t.Run("a wrapped validation error is unwrapped to the same pointer", func(t *testing.T) {
		valErr := &destregistry.ErrDestinationValidation{
			Errors: []destregistry.ValidationErrorDetail{{Field: "config.url", Type: "required"}},
		}
		provider.validateErr = fmt.Errorf("provider: %w", valErr)
		err := registry.ValidateDestination(context.Background(), dest)
		assert.Same(t, valErr, err)
	})

	t.Run("other errors stay opaque", func(t *testing.T) {
		provider.validateErr = errors.New("dial tcp 10.0.0.1:443: connection refused")
		err := registry.ValidateDestination(context.Background(), dest)
		var valErr *destregistry.ErrDestinationValidation
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "root", Type: "unknown"}}, valErr.Errors)
		assert.Nil(t, valErr.Cause, "internal error details never leak through Cause")
	})
}

package destregistry_test

import (
	"context"
	"errors"
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
	*destregistry.BaseProvider
	publishDelay time.Duration
	mockError    error
}

type mockPublisher struct {
	id           int64
	closed       bool
	publishDelay time.Duration
	mockError    error
}

var mockPublisherID int64

func newMockPublisher() *mockPublisher {
	return &mockPublisher{
		id: atomic.AddInt64(&mockPublisherID, 1),
	}
}

type mockMetadataLoader struct{}

func (m *mockMetadataLoader) Load(providerType string) (*metadata.ProviderMetadata, error) {
	return &metadata.ProviderMetadata{
		Type: "mock",
		ConfigFields: []metadata.FieldSchema{
			{
				Key:       "public_key",
				Type:      "string",
				Required:  true,
				Sensitive: false,
			},
			{
				Key:       "secret_key",
				Type:      "string",
				Required:  true,
				Sensitive: true,
			},
		},
		CredentialFields: []metadata.FieldSchema{
			{
				Key:       "api_key",
				Type:      "string",
				Required:  true,
				Sensitive: true,
			},
			{
				Key:       "token",
				Type:      "string",
				Required:  false,
				Sensitive: true,
			},
			{
				Key:       "code",
				Type:      "string",
				Required:  false,
				Sensitive: true,
			},
		},
	}, nil
}

func newMockProvider() (*mockProvider, error) {
	base, err := destregistry.NewBaseProvider(&mockMetadataLoader{}, "mock")
	if err != nil {
		return nil, err
	}

	return &mockProvider{
		BaseProvider: base,
	}, nil
}

func (p *mockProvider) Validate(ctx context.Context, dest *models.Destination) error { return nil }

func (p *mockProvider) CreatePublisher(ctx context.Context, dest *models.Destination) (destregistry.Publisher, error) {
	atomic.AddInt32(&p.createCount, 1)
	pub := newMockPublisher()
	pub.publishDelay = p.publishDelay
	pub.mockError = p.mockError
	return pub, nil
}

func (p *mockProvider) ComputeTarget(dest *models.Destination) string {
	return "mock-target"
}

func (p *mockPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	select {
	case <-time.After(p.publishDelay):
		if p.mockError != nil {
			return nil, p.mockError
		}
		return &destregistry.Delivery{
			Status:   "success",
			Code:     "OK",
			Response: map[string]interface{}{"msg": "published"},
		}, nil
	case <-ctx.Done():
		if p.mockError != nil {
			return nil, p.mockError
		}
		return nil, ctx.Err()
	}
}

func (p *mockPublisher) Close() error {
	p.closed = true
	return nil
}

func TestRegistryConcurrentPublisherManagement(t *testing.T) {
	testutil.Race(t)

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	provider, err := newMockProvider()
	require.NoError(t, err)
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

func (p *mockPublisherWithConfig) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	target := publishTarget{
		providerType: p.providerType,
		config:       p.config,
	}
	publishedEvents[target] = append(publishedEvents[target], *event)
	return &destregistry.Delivery{
		Status:   "success",
		Code:     "OK",
		Response: map[string]interface{}{"msg": "published"},
	}, nil
}

func (p *mockPublisherWithConfig) Close() error { return nil }

type mockProviderWithConfig struct {
	providerType string
	preprocessFn func(*models.Destination, *models.Destination, *destregistry.PreprocessDestinationOpts) error
	*destregistry.BaseProvider
}

func newMockProviderWithConfig(providerType string) (*mockProviderWithConfig, error) {
	base, err := destregistry.NewBaseProvider(&mockMetadataLoader{}, providerType)
	if err != nil {
		return nil, err
	}
	return &mockProviderWithConfig{
		providerType: providerType,
		BaseProvider: base,
	}, nil
}

func (p *mockProviderWithConfig) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	return &mockPublisherWithConfig{
		providerType: p.providerType,
		config:       destination.Config["id"],
	}, nil
}

func (p *mockProviderWithConfig) ComputeTarget(destination *models.Destination) string {
	return "mock-target"
}

func (p *mockProviderWithConfig) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	if p.preprocessFn != nil {
		return p.preprocessFn(newDestination, originalDestination, opts)
	}
	return nil
}

func TestDestinationChanges(t *testing.T) {
	t.Parallel()
	t.Run("config change", func(t *testing.T) {
		publishedEvents = make(map[publishTarget][]models.Event)
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		provider, err := newMockProviderWithConfig("mock1")
		require.NoError(t, err)
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
		_, err = registry.PublishEvent(context.Background(), dest, event1)
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
		_, err = registry.PublishEvent(context.Background(), destUpdated, event2)
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
		provider1, err := newMockProviderWithConfig("mock1")
		require.NoError(t, err)
		provider2, err := newMockProviderWithConfig("mock2")
		require.NoError(t, err)
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
		_, err = registry.PublishEvent(context.Background(), dest, event1)
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
		_, err = registry.PublishEvent(context.Background(), destUpdated, event2)
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
		provider, err := newMockProvider()
		require.NoError(t, err)
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
		provider, err := newMockProvider()
		require.NoError(t, err)
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
		provider, err := newMockProvider()
		require.NoError(t, err)
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
		provider, err := newMockProvider()
		require.NoError(t, err)
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
	provider, err := newMockProvider()
	require.NoError(t, err)
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

func TestObfuscateValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single character",
			input:    "a",
			expected: "*",
		},
		{
			name:     "short string",
			input:    "abc123",
			expected: "******",
		},
		{
			name:     "9 characters",
			input:    "123456789",
			expected: "*********",
		},
		{
			name:     "10 characters",
			input:    "1234567890",
			expected: "1234******",
		},
		{
			name:     "long string",
			input:    "abcdefghijklmnop",
			expected: "abcd************",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := destregistry.ObfuscateValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestObfuscateDestination(t *testing.T) {
	t.Parallel()

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	provider, err := newMockProvider()
	require.NoError(t, err)
	registry.RegisterProvider("mock", provider)

	dest := &models.Destination{
		ID:   "test-dest",
		Type: "mock",
		Config: map[string]string{
			"public_key": "visible-value",
			"secret_key": "sensitive-value-123",
		},
		Credentials: map[string]string{
			"api_key": "abcdefghijklmnop",
			"token":   "xyz",
			"code":    "1234",
		},
	}

	obfuscated, err := registry.DisplayDestination(dest)
	require.NoError(t, err)

	// Original destination should be unchanged
	assert.Equal(t, "sensitive-value-123", dest.Config["secret_key"])
	assert.Equal(t, "abcdefghijklmnop", dest.Credentials["api_key"])

	// Non-sensitive fields should be unchanged
	assert.Equal(t, "visible-value", obfuscated.Config["public_key"])

	// Sensitive fields should be obfuscated according to length:
	// - Less than 10 chars: replace each character with an asterisk
	// - 10+ chars: first 4 chars + asterisks
	assert.Equal(t, "sens***************", obfuscated.Config["secret_key"]) // 19 chars
	assert.Equal(t, "abcd************", obfuscated.Credentials["api_key"])  // 16 chars
	assert.Equal(t, "***", obfuscated.Credentials["token"])                 // 3 chars
	assert.Equal(t, "****", obfuscated.Credentials["code"])                 // 4 chars
}

func TestPublishEventTimeout(t *testing.T) {
	timeout := 100 * time.Millisecond
	logger := testutil.CreateTestLogger(t)

	t.Run("should not return timeout error when publish completes within timeout", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{
			DeliveryTimeout: timeout,
		}, logger)

		provider, err := newMockProvider()
		require.NoError(t, err)
		provider.publishDelay = timeout / 2
		err = registry.RegisterProvider("test", provider)
		require.NoError(t, err)

		destination := &models.Destination{
			Type: "test",
		}
		event := &models.Event{}

		_, err = registry.PublishEvent(context.Background(), destination, event)
		assert.NoError(t, err)
	})

	t.Run("should return timeout error when publish exceeds timeout", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{
			DeliveryTimeout: timeout,
		}, logger)

		provider, err := newMockProvider()
		require.NoError(t, err)
		provider.publishDelay = timeout * 2
		err = registry.RegisterProvider("test", provider)
		require.NoError(t, err)

		destination := &models.Destination{
			Type: "test",
		}
		event := &models.Event{}

		_, err = registry.PublishEvent(context.Background(), destination, event)
		assert.Error(t, err)

		var publishErr *destregistry.ErrDestinationPublishAttempt
		assert.ErrorAs(t, err, &publishErr)
		assert.Equal(t, "test", publishErr.Provider)
		assert.Equal(t, "timeout", publishErr.Data["error"])
		assert.Equal(t, timeout.String(), publishErr.Data["timeout"])
	})

	t.Run("should handle wrapped timeout error from provider", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{
			DeliveryTimeout: timeout,
		}, logger)

		provider, err := newMockProvider()
		require.NoError(t, err)
		provider.publishDelay = timeout * 2
		provider.mockError = destregistry.NewErrDestinationPublishAttempt(context.DeadlineExceeded, "test", map[string]interface{}{"error": context.DeadlineExceeded})
		err = registry.RegisterProvider("test", provider)
		require.NoError(t, err)

		destination := &models.Destination{
			Type: "test",
		}
		event := &models.Event{}

		_, err = registry.PublishEvent(context.Background(), destination, event)
		assert.Error(t, err)

		var publishErr *destregistry.ErrDestinationPublishAttempt
		assert.ErrorAs(t, err, &publishErr)
		assert.Equal(t, "test", publishErr.Provider)
		assert.Equal(t, "timeout", publishErr.Data["error"])
		assert.Equal(t, timeout.String(), publishErr.Data["timeout"])
	})
}

func TestDisplayDestination(t *testing.T) {
	t.Parallel()

	dest := &models.Destination{
		Type: "mock",
		Config: map[string]string{
			"public_key": "value",
			"secret_key": "secret",
		},
		Credentials: map[string]string{
			"api_key": "secret-key",
		},
	}

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	provider, err := newMockProviderWithConfig("mock")
	require.NoError(t, err)
	err = registry.RegisterProvider("mock", provider)
	require.NoError(t, err)

	display, err := registry.DisplayDestination(dest)
	require.NoError(t, err)
	assert.Equal(t, "mock-target", display.Target)
	assert.Equal(t, "value", display.Config["public_key"])
	assert.Equal(t, "******", display.Config["secret_key"])
	assert.Equal(t, "secr******", display.Credentials["api_key"])
}

func TestPreprocessDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		destination *models.Destination
		preprocess  func(*models.Destination, *models.Destination, *destregistry.PreprocessDestinationOpts) error
		wantErr     bool
	}{
		{
			name: "noop preprocess",
			destination: &models.Destination{
				Type: "mock",
				Config: map[string]string{
					"key": "value",
				},
			},
			preprocess: nil, // Use base provider's implementation
			wantErr:    false,
		},
		{
			name: "modify config",
			destination: &models.Destination{
				Type: "mock",
				Config: map[string]string{
					"key": "value",
				},
			},
			preprocess: func(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
				newDestination.Config["processed"] = "true"
				return nil
			},
			wantErr: false,
		},
		{
			name: "preprocess error",
			destination: &models.Destination{
				Type: "mock",
				Config: map[string]string{
					"key": "value",
				},
			},
			preprocess: func(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
				return errors.New("preprocess error")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
			provider, err := newMockProviderWithConfig("mock")
			require.NoError(t, err)

			if tt.preprocess != nil {
				provider.preprocessFn = tt.preprocess
			}

			err = registry.RegisterProvider("mock", provider)
			require.NoError(t, err)

			err = registry.PreprocessDestination(tt.destination, nil, &destregistry.PreprocessDestinationOpts{})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.name == "modify config" {
				assert.Equal(t, "true", tt.destination.Config["processed"])
			}
		})
	}
}

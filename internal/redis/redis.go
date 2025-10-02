package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"

	"github.com/redis/go-redis/extra/redisotel/v9"
	r "github.com/redis/go-redis/v9"
)

// Reexport go-redis's Nil constant for DX purposes.
const (
	Nil = r.Nil
)

type (
	Cmdable            = r.Cmdable
	MapStringStringCmd = r.MapStringStringCmd
	Pipeliner          = r.Pipeliner
	Tx                 = r.Tx
)

type Client interface {
	Cmdable
	Close() error
}

const (
	TxFailedErr = r.TxFailedErr
)

var (
	once                sync.Once
	client              Client
	initializationError error
)

func New(ctx context.Context, config *RedisConfig) (r.Cmdable, error) {
	once.Do(func() {
		initializeClient(ctx, config)
		if initializationError == nil {
			initializationError = instrumentOpenTelemetry()
		}
	})

	// Ensure we never return nil client without an error
	if client == nil && initializationError == nil {
		initializationError = fmt.Errorf("redis client initialization failed: unexpected state")
	}

	return client, initializationError
}

// NewClient creates a new Redis client without using the singleton
// This should be used by components that need their own Redis connection,
// such as libraries or in test scenarios where isolation is required
func NewClient(ctx context.Context, config *RedisConfig) (r.Cmdable, error) {
	if config.ClusterEnabled {
		return createClusterClient(ctx, config)
	}
	return createRegularClient(ctx, config)
}

func createClusterClient(ctx context.Context, config *RedisConfig) (Client, error) {
	// Start with single node - cluster client will auto-discover other nodes
	options := &r.ClusterOptions{
		Addrs:    []string{fmt.Sprintf("%s:%d", config.Host, config.Port)},
		Password: config.Password,
		// Note: Database is ignored in cluster mode
	}

	// Development only: Override discovered node IPs with the original host
	// This is needed for Docker environments where Redis nodes announce internal IPs
	if config.DevClusterHostOverride {
		originalHost := config.Host
		options.NewClient = func(opt *r.Options) *r.Client {
			// Extract port from discovered address and combine with original host
			if idx := strings.LastIndex(opt.Addr, ":"); idx > 0 {
				port := opt.Addr[idx:] // includes the colon
				opt.Addr = originalHost + port
			}
			return r.NewClient(opt)
		}
	}

	if config.TLSEnabled {
		options.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		}
	}

	clusterClient := r.NewClusterClient(options)

	// Test connectivity
	if err := clusterClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("cluster client ping failed: %w", err)
	}

	return clusterClient, nil
}

func createRegularClient(ctx context.Context, config *RedisConfig) (Client, error) {
	options := &r.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.Database,
	}

	if config.TLSEnabled {
		options.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		}
	}

	regularClient := r.NewClient(options)

	// Test connectivity
	if err := regularClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("regular client ping failed: %w", err)
	}

	return regularClient, nil
}

func instrumentOpenTelemetry() error {
	// OpenTelemetry instrumentation requires a concrete client type for type assertions
	if concreteClient, ok := client.(*r.Client); ok {
		if err := redisotel.InstrumentTracing(concreteClient); err != nil {
			return err
		}
	} else if clusterClient, ok := client.(*r.ClusterClient); ok {
		if err := redisotel.InstrumentTracing(clusterClient); err != nil {
			return err
		}
	}
	return nil
}

func initializeClient(ctx context.Context, config *RedisConfig) {
	var err error
	if config.ClusterEnabled {
		client, err = createClusterClient(ctx, config)
		if err != nil {
			initializationError = fmt.Errorf("redis cluster connection failed: %w", err)
			return
		}
	} else {
		client, err = createRegularClient(ctx, config)
		if err != nil {
			initializationError = fmt.Errorf("redis regular client connection failed: %w", err)
			return
		}
	}
}

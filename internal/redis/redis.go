package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/redis/go-redis/extra/redisotel/v9"
	r "github.com/redis/go-redis/v9"
)

// Reexport go-redis's Nil constant for DX purposes.
const (
	Nil = r.Nil
)

type (
	Client             = r.Client
	Cmdable            = r.Cmdable
	MapStringStringCmd = r.MapStringStringCmd
	Pipeliner          = r.Pipeliner
	Tx                 = r.Tx
)

const (
	TxFailedErr = r.TxFailedErr
)

var (
	once                sync.Once
	client              *r.Client
	initializationError error
)

func New(ctx context.Context, config *RedisConfig) (*r.Client, error) {
	once.Do(func() {
		initializeClient(ctx, config)
		initializationError = instrumentOpenTelemetry()
	})
	return client, initializationError
}

// NewClient is a helper function to create a new redis client without the singleton initialization.
// This is useful for testing purposes and when cluster mode is needed.
func NewClient(ctx context.Context, config *RedisConfig) (*r.Client, error) {
	if config.ClusterEnabled {
		return newClusterClient(ctx, config)
	}
	return newRegularClient(ctx, config)
}

func newClusterClient(ctx context.Context, config *RedisConfig) (*r.Client, error) {
	// For now, create a regular client that can connect to cluster nodes
	// This is a limitation of mixing v9 redis package with older packages
	options := &r.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		// Database is ignored in cluster mode, but we'll set it anyway
		DB: config.Database,
	}
	
	if config.TLSEnabled {
		options.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Azure Managed Redis uses self-signed certificates
		}
	}
	
	regularClient := r.NewClient(options)
	
	// Test connectivity
	if err := regularClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("Redis client connection failed (cluster mode requested but using regular client for compatibility): %w", err)
	}
	
	return regularClient, nil
}

func newRegularClient(ctx context.Context, config *RedisConfig) (*r.Client, error) {
	options := &r.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.Database,
	}
	
	if config.TLSEnabled {
		options.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Azure Managed Redis uses self-signed certificates
		}
	}
	
	regularClient := r.NewClient(options)
	
	// Test regular client connectivity
	if err := regularClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("regular client connection failed: %w", err)
	}
	
	return regularClient, nil
}

func instrumentOpenTelemetry() error {
	if err := redisotel.InstrumentTracing(client); err != nil {
		return err
	}
	return nil
}

func initializeClient(_ context.Context, config *RedisConfig) {
	if config.ClusterEnabled {
		options := &r.ClusterOptions{
			Addrs:    []string{fmt.Sprintf("%s:%d", config.Host, config.Port)},
			Password: config.Password,
			// Note: Database is ignored in cluster mode
		}
		
		if config.TLSEnabled {
			options.TLSConfig = &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, // Azure Managed Redis uses self-signed certificates
			}
		}
		
		// For now, we can't easily support cluster in singleton mode with current architecture
		// Fall back to regular client - this will be addressed in scheduler update
		client = r.NewClient(&r.Options{
			Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
			Password: config.Password,
			DB:       config.Database,
			TLSConfig: options.TLSConfig,
		})
	} else {
		options := &r.Options{
			Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
			Password: config.Password,
			DB:       config.Database,
		}
		
		if config.TLSEnabled {
			options.TLSConfig = &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, // Azure Managed Redis uses self-signed certificates
			}
		}
		
		client = r.NewClient(options)
	}
}

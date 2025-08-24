package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/redis/go-redis/extra/redisotel/v9"
	r "github.com/redis/go-redis/v9"
	oldredis "github.com/go-redis/redis"
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
	client              r.Cmdable  // Use interface to support both regular and cluster clients
	initializationError error
)

func New(ctx context.Context, config *RedisConfig) (r.Cmdable, error) {
	once.Do(func() {
		initializeClient(ctx, config)
		initializationError = instrumentOpenTelemetry()
	})
	return client, initializationError
}

// NewForTest creates a new Redis client for testing without using the singleton
func NewForTest(ctx context.Context, config *RedisConfig) (r.Cmdable, error) {
	if config.ClusterEnabled {
		return createClusterClient(ctx, config)
	}
	return createRegularClient(ctx, config)
}

func createClusterClient(ctx context.Context, config *RedisConfig) (r.Cmdable, error) {
	options := &r.ClusterOptions{
		Addrs:    []string{fmt.Sprintf("%s:%d", config.Host, config.Port)},
		Password: config.Password,
		// Note: Database is ignored in cluster mode
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

func createRegularClient(ctx context.Context, config *RedisConfig) (r.Cmdable, error) {
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

// NewClientForScheduler creates a Redis client specifically for scheduler/RSMQ usage
// This uses the old Redis package for compatibility with RSMQ  
func NewClientForScheduler(ctx context.Context, config *RedisConfig) (interface{}, error) {
	if config.ClusterEnabled {
		return newOldClusterClient(ctx, config)
	}
	return newOldRegularClient(ctx, config)
}

func newOldClusterClient(ctx context.Context, config *RedisConfig) (interface{}, error) {
	options := &oldredis.ClusterOptions{
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
	
	clusterClient := oldredis.NewClusterClient(options)
	
	// Test connectivity
	if err := clusterClient.Ping().Err(); err != nil {
		return nil, fmt.Errorf("Redis cluster client connection failed (old package): %w", err)
	}
	
	return clusterClient, nil
}

func newOldRegularClient(ctx context.Context, config *RedisConfig) (interface{}, error) {
	options := &oldredis.Options{
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
	
	regularClient := oldredis.NewClient(options)
	
	// Test connectivity
	if err := regularClient.Ping().Err(); err != nil {
		return nil, fmt.Errorf("Redis regular client connection failed (old package): %w", err)
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
	if config.ClusterEnabled {
		// Create proper cluster client for cluster deployments
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
		
		clusterClient := r.NewClusterClient(options)
		
		// Test the cluster client connectivity
		if err := clusterClient.Ping(ctx).Err(); err != nil {
			initializationError = fmt.Errorf("Redis cluster connection failed: %w", err)
			return
		}
		
		// Assign to interface
		client = clusterClient
	} else {
		// Create regular client for non-cluster deployments
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
		
		// Test the regular client connectivity
		if err := regularClient.Ping(ctx).Err(); err != nil {
			panic(fmt.Sprintf("Redis regular client connection failed: %v", err))
		}
		
		// Assign to interface
		client = regularClient
	}
}

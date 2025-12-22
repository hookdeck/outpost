package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

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
	Cmd                = r.Cmd
)

type Client interface {
	Cmdable
	Close() error
}

// DoContext is an interface for executing arbitrary Redis commands.
// This is needed for RediSearch commands that aren't in the Cmdable interface.
type DoContext interface {
	Do(ctx context.Context, args ...interface{}) *r.Cmd
}

const (
	TxFailedErr = r.TxFailedErr
)

func New(ctx context.Context, config *RedisConfig) (Client, error) {
	var client Client
	var err error

	if config.ClusterEnabled {
		client, err = createClusterClient(ctx, config)
	} else {
		client, err = createRegularClient(ctx, config)
	}

	if err != nil {
		return nil, err
	}

	if err := instrumentOpenTelemetry(client); err != nil {
		return nil, err
	}

	return client, nil
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

func instrumentOpenTelemetry(client Client) error {
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

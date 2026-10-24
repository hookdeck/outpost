package redis

import (
	"context"
	"net"
)

type RedisConfig struct {
	Host           string
	Port           int
	Username       string
	Password       string
	Database       int
	TLSEnabled     bool
	TLSVerify      bool
	ClusterEnabled bool

	// PoolSize is the connection pool size per client (per node in cluster
	// mode). 0 uses go-redis's default of 10 per GOMAXPROCS.
	PoolSize int

	// DevClusterHostOverride when true, forces cluster node discovery to use the
	// original Host value instead of discovered IPs. This is a development-only
	// setting for Docker environments where nodes announce unreachable IPs.
	// DO NOT use in production.
	DevClusterHostOverride bool

	// Dialer overrides how the regular (non-cluster) client opens connections.
	// Tests use it to reach an in-memory server; it is never set from config.
	// Setting it also disables maintenance notifications.
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)
}

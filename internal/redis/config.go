package redis

type RedisConfig struct {
	Host           string
	Port           int
	Username       string
	Password       string
	Database       int
	TLSEnabled     bool
	ClusterEnabled bool

	// DevClusterHostOverride when true, forces cluster node discovery to use the
	// original Host value instead of discovered IPs. This is a development-only
	// setting for Docker environments where nodes announce unreachable IPs.
	// DO NOT use in production.
	DevClusterHostOverride bool
}

package redis

type RedisConfig struct {
	Host           string
	Port           int
	Password       string
	Database       int
	TLSEnabled     bool
	ClusterEnabled bool
}

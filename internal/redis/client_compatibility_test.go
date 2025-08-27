package redis

import (
	"testing"

	"github.com/go-redis/redis"
)

// TestRedisClientInterfaceCompatibility verifies that both regular and cluster clients
// implement the RedisClient interface to ensure backward compatibility
func TestRedisClientInterfaceCompatibility(t *testing.T) {
	// Create a regular Redis client
	regularClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer regularClient.Close()

	// Create a cluster Redis client
	clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs: []string{"localhost:6379"},
	})
	defer clusterClient.Close()

	// Test that regular client implements all RedisClient interface methods
	t.Run("RegularClientImplementsInterface", func(t *testing.T) {
		// This will fail at compile time if the interface is not implemented
		var _ RedisClient = regularClient
	})

	// Test that cluster client implements all RedisClient interface methods
	t.Run("ClusterClientImplementsInterface", func(t *testing.T) {
		// This will fail at compile time if the interface is not implemented  
		var _ RedisClient = clusterClient
	})
}

// RedisClient interface definition for testing
// This should match the interface in internal/rsmq/rsmq.go
type RedisClient interface {
	Time() *redis.TimeCmd
	HSetNX(key, field string, value interface{}) *redis.BoolCmd
	HMGet(key string, fields ...string) *redis.SliceCmd
	SMembers(key string) *redis.StringSliceCmd
	SAdd(key string, members ...interface{}) *redis.IntCmd
	ZCard(key string) *redis.IntCmd
	ZCount(key, min, max string) *redis.IntCmd
	ZAdd(key string, members ...redis.Z) *redis.IntCmd
	HSet(key, field string, value interface{}) *redis.BoolCmd
	HIncrBy(key, field string, incr int64) *redis.IntCmd
	Del(keys ...string) *redis.IntCmd
	HDel(key string, fields ...string) *redis.IntCmd
	ZRem(key string, members ...interface{}) *redis.IntCmd
	SRem(key string, members ...interface{}) *redis.IntCmd
	EvalSha(sha1 string, keys []string, args ...interface{}) *redis.Cmd
	ScriptLoad(script string) *redis.StringCmd
	TxPipeline() redis.Pipeliner
	Exists(keys ...string) *redis.IntCmd
	Type(key string) *redis.StatusCmd
	Close() error
}
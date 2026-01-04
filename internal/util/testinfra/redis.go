package testinfra

import (
	"context"
	"fmt"
	"log"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go/modules/redis"
)

var (
	redisOnce   sync.Once
	redisDBMu   sync.Mutex
	redisDBUsed = make(map[int]bool)
)

const maxRedisDBs = 16

// RedisConfig holds the connection info for a test Redis database.
type RedisConfig struct {
	Addr string
	DB   int
}

// NewRedisConfig allocates a Redis database (0-15) for the test.
// The database is flushed on cleanup.
func NewRedisConfig(t *testing.T) RedisConfig {
	addr := EnsureRedis()
	db := allocateRedisDB()

	cfg := RedisConfig{
		Addr: addr,
		DB:   db,
	}

	t.Cleanup(func() {
		flushRedisDB(addr, db)
		releaseRedisDB(db)
	})

	return cfg
}

func allocateRedisDB() int {
	redisDBMu.Lock()
	defer redisDBMu.Unlock()

	for i := 0; i < maxRedisDBs; i++ {
		if !redisDBUsed[i] {
			redisDBUsed[i] = true
			return i
		}
	}
	panic(fmt.Sprintf("no available Redis databases (max %d)", maxRedisDBs))
}

func releaseRedisDB(db int) {
	redisDBMu.Lock()
	defer redisDBMu.Unlock()
	delete(redisDBUsed, db)
}

func flushRedisDB(addr string, db int) {
	client := goredis.NewClient(&goredis.Options{
		Addr: addr,
		DB:   db,
	})
	defer client.Close()

	ctx := context.Background()
	if err := client.FlushDB(ctx).Err(); err != nil {
		log.Printf("failed to flush Redis DB %d: %s", db, err)
	}
}

func EnsureRedis() string {
	cfg := ReadConfig()
	if cfg.RedisURL == "" {
		redisOnce.Do(func() {
			startRedisTestContainer(cfg)
		})
	}
	return cfg.RedisURL
}

func startRedisTestContainer(cfg *Config) {
	ctx := context.Background()

	redisContainer, err := redis.Run(ctx,
		"redis/redis-stack-server:latest",
	)
	if err != nil {
		panic(err)
	}

	endpoint, err := redisContainer.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		panic(err)
	}
	log.Printf("Redis (redis-stack-server) running at %s", endpoint)
	cfg.RedisURL = endpoint
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate container: %s", err)
		}
	})
}

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
	redisOnce       sync.Once
	dragonflyOnce   sync.Once
	redisDBMu       sync.Mutex
	dragonflyDBMu   sync.Mutex
	redisDBUsed     = make(map[int]bool)
	dragonflyDBUsed = make(map[int]bool)
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
	db := allocateDB(&redisDBMu, redisDBUsed)

	cfg := RedisConfig{
		Addr: addr,
		DB:   db,
	}

	t.Cleanup(func() {
		flushRedisDB(addr, db)
		releaseDB(&redisDBMu, redisDBUsed, db)
	})

	return cfg
}

// NewDragonflyConfig allocates a Dragonfly database (0-15) for the test.
// The database is flushed on cleanup.
func NewDragonflyConfig(t *testing.T) RedisConfig {
	addr := EnsureDragonfly()
	db := allocateDB(&dragonflyDBMu, dragonflyDBUsed)

	cfg := RedisConfig{
		Addr: addr,
		DB:   db,
	}

	t.Cleanup(func() {
		flushRedisDB(addr, db)
		releaseDB(&dragonflyDBMu, dragonflyDBUsed, db)
	})

	return cfg
}

func allocateDB(mu *sync.Mutex, used map[int]bool) int {
	mu.Lock()
	defer mu.Unlock()

	for i := 0; i < maxRedisDBs; i++ {
		if !used[i] {
			used[i] = true
			return i
		}
	}
	panic(fmt.Sprintf("no available databases (max %d)", maxRedisDBs))
}

func releaseDB(mu *sync.Mutex, used map[int]bool, db int) {
	mu.Lock()
	defer mu.Unlock()
	delete(used, db)
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

func EnsureDragonfly() string {
	cfg := ReadConfig()
	if cfg.DragonflyURL == "" {
		dragonflyOnce.Do(func() {
			startDragonflyTestContainer(cfg)
		})
	}
	return cfg.DragonflyURL
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

func startDragonflyTestContainer(cfg *Config) {
	ctx := context.Background()

	// Use the redis module with Dragonfly image
	dragonflyContainer, err := redis.Run(ctx,
		"docker.dragonflydb.io/dragonflydb/dragonfly:latest",
	)
	if err != nil {
		panic(err)
	}

	endpoint, err := dragonflyContainer.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		panic(err)
	}
	log.Printf("Dragonfly running at %s", endpoint)
	cfg.DragonflyURL = endpoint
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := dragonflyContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate container: %s", err)
		}
	})
}

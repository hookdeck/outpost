package testinfra

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/redis"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

var (
	redisOnce     sync.Once
	dragonflyOnce sync.Once

	// DB 0 is reserved for Redis Stack / Dragonfly Stack (RediSearch requires DB 0)
	// Tests needing RediSearch serialize via these mutexes
	redisStackMu     sync.Mutex
	dragonflyStackMu sync.Mutex

	// DB 1-15 are for regular Redis/Dragonfly tests (can run in parallel)
	redisDBMu       sync.Mutex
	dragonflyDBMu   sync.Mutex
	redisDBUsed     = make(map[int]bool)
	dragonflyDBUsed = make(map[int]bool)
)

const (
	redisStackDB = 0 // Reserved for Redis Stack (RediSearch)
	minRegularDB = 1 // Regular tests use DB 1-15
	maxRegularDB = 15
)

// NewRedisConfig allocates a Redis database (1-15) for the test.
// Use this for tests that don't need RediSearch.
// The database is flushed on cleanup.
func NewRedisConfig(t *testing.T) *redis.RedisConfig {
	addr := EnsureRedis()
	db := allocateRegularDB()

	cfg := parseAddrToConfig(addr, db)

	t.Cleanup(func() {
		flushRedisDB(addr, db)
		releaseRegularDB(db)
	})

	return cfg
}

// NewRedisStackConfig returns DB 0 for tests requiring RediSearch.
// Tests using this are serialized (one at a time) since RediSearch only works on DB 0.
// The database is flushed on cleanup.
func NewRedisStackConfig(t *testing.T) *redis.RedisConfig {
	addr := EnsureRedis()

	// Acquire exclusive access to DB 0
	redisStackMu.Lock()

	cfg := parseAddrToConfig(addr, redisStackDB)

	t.Cleanup(func() {
		flushRedisDB(addr, redisStackDB)
		redisStackMu.Unlock()
	})

	return cfg
}

// NewDragonflyConfig allocates a Dragonfly database (1-15) for the test.
// Use NewDragonflyStackConfig for tests requiring RediSearch.
// The database is flushed on cleanup.
func NewDragonflyConfig(t *testing.T) *redis.RedisConfig {
	addr := EnsureDragonfly()
	db := allocateDragonflyDB()

	cfg := parseAddrToConfig(addr, db)

	t.Cleanup(func() {
		flushRedisDB(addr, db)
		releaseDragonflyDB(db)
	})

	return cfg
}

// NewDragonflyStackConfig returns DB 0 for tests requiring RediSearch on Dragonfly.
// Tests using this are serialized (one at a time) since RediSearch only works on DB 0.
// The database is flushed on cleanup.
func NewDragonflyStackConfig(t *testing.T) *redis.RedisConfig {
	addr := EnsureDragonfly()

	// Acquire exclusive access to DB 0
	dragonflyStackMu.Lock()

	cfg := parseAddrToConfig(addr, redisStackDB)

	t.Cleanup(func() {
		flushRedisDB(addr, redisStackDB)
		dragonflyStackMu.Unlock()
	})

	return cfg
}

// parseAddrToConfig converts an addr string (host:port) to a RedisConfig.
func parseAddrToConfig(addr string, db int) *redis.RedisConfig {
	parts := strings.Split(addr, ":")
	port := 6379 // default
	if len(parts) == 2 {
		if p, err := strconv.Atoi(parts[1]); err == nil {
			port = p
		}
	}
	return &redis.RedisConfig{
		Host:     parts[0],
		Port:     port,
		Database: db,
	}
}

func allocateRegularDB() int {
	redisDBMu.Lock()
	defer redisDBMu.Unlock()

	for i := minRegularDB; i <= maxRegularDB; i++ {
		if !redisDBUsed[i] {
			redisDBUsed[i] = true
			return i
		}
	}
	panic(fmt.Sprintf("no available Redis databases (DB %d-%d all in use)", minRegularDB, maxRegularDB))
}

func releaseRegularDB(db int) {
	redisDBMu.Lock()
	defer redisDBMu.Unlock()
	delete(redisDBUsed, db)
}

func allocateDragonflyDB() int {
	dragonflyDBMu.Lock()
	defer dragonflyDBMu.Unlock()

	// Start from DB 1; DB 0 is reserved for DragonflyStack (RediSearch)
	for i := minRegularDB; i <= maxRegularDB; i++ {
		if !dragonflyDBUsed[i] {
			dragonflyDBUsed[i] = true
			return i
		}
	}
	panic(fmt.Sprintf("no available Dragonfly databases (DB %d-%d all in use)", minRegularDB, maxRegularDB))
}

func releaseDragonflyDB(db int) {
	dragonflyDBMu.Lock()
	defer dragonflyDBMu.Unlock()
	delete(dragonflyDBUsed, db)
}

func flushRedisDB(addr string, db int) {
	cfg := parseAddrToConfig(addr, db)
	client, err := redis.New(context.Background(), cfg)
	if err != nil {
		log.Printf("failed to create Redis client for flush: %s", err)
		return
	}
	defer client.Close()

	if err := client.FlushDB(context.Background()).Err(); err != nil {
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

	redisContainer, err := rediscontainer.Run(ctx,
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
	dragonflyContainer, err := rediscontainer.Run(ctx,
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

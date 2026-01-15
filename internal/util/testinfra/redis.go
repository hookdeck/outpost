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
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	redisOnce     sync.Once
	dragonflyOnce sync.Once

	// DB 1-15 are for regular Redis/Dragonfly tests (can run in parallel)
	redisDBMu       sync.Mutex
	dragonflyDBMu   sync.Mutex
	redisDBUsed     = make(map[int]bool)
	dragonflyDBUsed = make(map[int]bool)
)

const (
	minRegularDB = 1 // Regular tests use DB 1-15
	maxRegularDB = 15
)

// RedisConfig holds the connection info for a test Redis database.
type RedisConfig struct {
	Addr string
	DB   int
}

// NewRedisConfig allocates a Redis database (0-15) for the test.
// The database is flushed on cleanup.
func NewRedisConfig(t *testing.T) RedisConfig {
	addr := EnsureRedis()
	db := allocateRegularDB()

	cfg := RedisConfig{
		Addr: addr,
		DB:   db,
	}

	t.Cleanup(func() {
		flushRedisDB(addr, db)
		releaseRegularDB(db)
	})

	return cfg
}

// NewRedisStackConfig spins up a dedicated Redis Stack container for tests requiring RediSearch.
// Each test gets its own isolated container, eliminating cross-test interference.
// The container is terminated on cleanup.
func NewRedisStackConfig(t *testing.T) *redis.RedisConfig {
	ctx := context.Background()

	container, err := rediscontainer.Run(ctx,
		"redis/redis-stack-server:latest",
	)
	if err != nil {
		t.Fatalf("failed to start redis-stack container: %v", err)
	}

	endpoint, err := container.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		t.Fatalf("failed to get redis-stack endpoint: %v", err)
	}
	log.Printf("Redis Stack (dedicated for test) running at %s", endpoint)

	cfg := parseAddrToConfig(endpoint, 0)

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate redis-stack container: %s", err)
		}
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

// NewDragonflyStackConfig spins up a dedicated Dragonfly container for tests requiring RediSearch.
// Each test gets its own isolated container, eliminating cross-test interference.
// The container is terminated on cleanup.
func NewDragonflyStackConfig(t *testing.T) *redis.RedisConfig {
	ctx := context.Background()

	// Use generic container instead of redis module since Dragonfly has different startup behavior.
	// Set proactor_threads=1 and maxmemory=256mb to reduce resource usage when running many containers.
	req := testcontainers.ContainerRequest{
		Image:        "docker.dragonflydb.io/dragonflydb/dragonfly:latest",
		ExposedPorts: []string{"6379/tcp"},
		Cmd:          []string{"--proactor_threads=1", "--maxmemory=256mb"},
		WaitingFor:   wait.ForListeningPort("6379/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start dragonfly container: %v", err)
	}

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("failed to get dragonfly endpoint: %v", err)
	}
	log.Printf("Dragonfly (dedicated for test) running at %s", endpoint)

	cfg := parseAddrToConfig(endpoint, 0)

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate dragonfly container: %s", err)
		}
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
	panic(fmt.Sprintf("no available databases (DB %d-%d all in use)", minRegularDB, maxRegularDB))
}

func releaseRegularDB(db int) {
	redisDBMu.Lock()
	defer redisDBMu.Unlock()
	delete(redisDBUsed, db)
}

func allocateDragonflyDB() int {
	dragonflyDBMu.Lock()
	defer dragonflyDBMu.Unlock()

	// Use DB 1-15 to avoid conflicts with tests that might use DB 0
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

	// Use generic container with resource-limiting flags to prevent memory issues.
	// Dragonfly requires ~256MB per thread by default, which can exhaust memory.
	req := testcontainers.ContainerRequest{
		Image:        "docker.dragonflydb.io/dragonflydb/dragonfly:latest",
		ExposedPorts: []string{"6379/tcp"},
		Cmd:          []string{"--proactor_threads=1", "--maxmemory=256mb"},
		WaitingFor:   wait.ForListeningPort("6379/tcp"),
	}

	dragonflyContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(err)
	}

	endpoint, err := dragonflyContainer.Endpoint(ctx, "")
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

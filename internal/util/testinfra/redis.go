package testinfra

import (
	"context"
	"log"
	"strconv"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/redis"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewRedisConfig spins up a dedicated Redis container for the test.
// Use this for tests that don't need RediSearch.
// The container is terminated on cleanup.
func NewRedisConfig(t *testing.T) *redis.RedisConfig {
	return startRedisContainer(t, "redis/redis-stack-server:latest")
}

// NewRedisStackConfig spins up a dedicated Redis Stack container for tests requiring RediSearch.
// Each test gets its own isolated container, eliminating cross-test interference.
// The container is terminated on cleanup.
func NewRedisStackConfig(t *testing.T) *redis.RedisConfig {
	return startRedisContainer(t, "redis/redis-stack-server:latest")
}

// NewDragonflyConfig spins up a dedicated Dragonfly container for the test.
// Each caller gets its own isolated container on DB 0, eliminating cross-test interference.
// The container is terminated on cleanup.
func NewDragonflyConfig(t *testing.T) *redis.RedisConfig {
	ctx := context.Background()

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
	log.Printf("Dragonfly (dedicated) running at %s", endpoint)

	cfg := parseAddrToConfig(endpoint, 0)

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate dragonfly container: %s", err)
		}
	})

	return cfg
}

// NewDragonflyStackConfig is an alias for NewDragonflyConfig.
// Kept for backward compatibility with existing test code.
func NewDragonflyStackConfig(t *testing.T) *redis.RedisConfig {
	return NewDragonflyConfig(t)
}

func startRedisContainer(t *testing.T, image string) *redis.RedisConfig {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("failed to get redis endpoint: %v", err)
	}
	log.Printf("Redis (%s) running at %s", image, endpoint)

	cfg := parseAddrToConfig(endpoint, 0)

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate redis container: %s", err)
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

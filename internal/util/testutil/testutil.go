package testutil

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/logging"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap/zaptest"
)

// must be sorted
var TestTopics = []string{
	"user.created",
	"user.deleted",
	"user.updated",
}

func CheckIntegrationTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
}

func Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
}

func Race(t *testing.T) {
	if os.Getenv("TESTRACE") != "1" {
		t.Skip("skipping race test")
	}
}

// Test groups beyond the default run: TESTDEST=1 adds the destination
// provider tests; TESTCOMPAT=1 adds Outpost on the other internal queues and
// the backend compat suites, and includes TESTDEST (it needs the same
// brokers). TESTDEST ⊂ TESTCOMPAT; TESTAZURE is separate (see testinfra).

// SkipUnlessCompat skips tests of alternative backends: internal queues other
// than NATS, and e2e suites on non-default stores.
func SkipUnlessCompat(t *testing.T) {
	t.Helper()
	if os.Getenv("TESTCOMPAT") != "1" {
		t.Skip("skipping compat test (set TESTCOMPAT=1 to run)")
	}
}

// SkipUnlessDest skips destination provider tests, which need the destination
// stack (make up/dest). TESTCOMPAT=1 runs them too.
func SkipUnlessDest(t *testing.T) {
	t.Helper()
	if os.Getenv("TESTDEST") != "1" && os.Getenv("TESTCOMPAT") != "1" {
		t.Skip("skipping destination test (set TESTDEST=1 to run)")
	}
}

func CreateTestRedisConfig(t *testing.T) *internalredis.RedisConfig {
	mr := miniredis.RunT(t)

	t.Cleanup(func() {
		mr.Close()
	})

	port, _ := strconv.Atoi(mr.Port())

	return &internalredis.RedisConfig{
		Host:     mr.Host(),
		Port:     port,
		Password: "",
		Database: 0,
	}
}

// NewSynctestRedis returns an in-memory Redis for tests inside a synctest
// bubble: no listener, and TTLs follow the fake clock. The config's Dialer
// connects to it, so redis.New(ctx, cfg) works in a bubble too.
func NewSynctestRedis(t *testing.T) (*miniredis.Miniredis, *internalredis.RedisConfig, internalredis.Client) {
	t.Helper()
	mr := miniredis.NewMiniRedis()
	mr.ClockTTL(true)

	// go-redis can finish a background dial after the client closes; the
	// lock keeps that dial from racing with mr.Close.
	var mu sync.Mutex
	closed := false
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		closed = true
		mr.Close()
	})

	cfg := &internalredis.RedisConfig{
		Host: "127.0.0.1",
		Port: 6379,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			mu.Lock()
			defer mu.Unlock()
			if closed {
				return nil, net.ErrClosed
			}
			return mr.Dial()
		},
	}
	client, err := internalredis.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create synctest redis client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return mr, cfg, client
}

func CreateTestRedisClient(t *testing.T) internalredis.Client {
	mr := miniredis.RunT(t)

	t.Cleanup(func() {
		mr.Close()
	})

	return redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
}

func CreateTestLogger(t *testing.T) *logging.Logger {
	zapLogger := zaptest.NewLogger(t)
	return logging.NewTestLogger(zapLogger)
}

func RandomString(length int) string {
	b := make([]byte, length+2)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[2 : length+2]
}

func RandomPortNumber() int {
	return 10000 + mathrand.Intn(50000)
}

// RandomPort returns a random port string in the range :10000–:59999.
func RandomPort() string {
	return ":" + strconv.Itoa(RandomPortNumber())
}

func MustMarshalJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

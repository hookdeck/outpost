package testutil

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"net"
	"os"
	"strconv"
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

// Random ports are picked below the kernel's default ephemeral range
// (32768–60999 on Linux), where outgoing connections, httptest servers and
// Docker's published ports take theirs: a port from that range could be in
// use, or taken before the test listens on it.
const (
	minRandomPort = 10000
	maxRandomPort = 32767
)

// RandomPortNumber returns a random port in the range 10000–32767 that is
// free when it returns (another listener can still take it before the
// caller does).
func RandomPortNumber() int {
	port := minRandomPort + mathrand.Intn(maxRandomPort-minRandomPort+1)
	for range 20 {
		if l, err := net.Listen("tcp", ":"+strconv.Itoa(port)); err == nil {
			l.Close()
			return port
		}
		port = minRandomPort + mathrand.Intn(maxRandomPort-minRandomPort+1)
	}
	return port
}

// RandomPort returns a random port string in the range :10000–:32767.
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

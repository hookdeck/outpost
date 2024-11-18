package testutil

import (
	"crypto/rand"
	"fmt"
	mathrand "math/rand"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

var TestTopics = []string{
	"user.created",
	"user.updated",
	"user.deleted",
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

func CreateTestRedisClient(t *testing.T) *redis.Client {
	mr := miniredis.RunT(t)

	t.Cleanup(func() {
		mr.Close()
	})

	return redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
}

func CreateTestLogger(t *testing.T) *otelzap.Logger {
	zapLogger := zaptest.NewLogger(t)
	logger := otelzap.New(zapLogger,
		otelzap.WithMinLevel(zap.InfoLevel),
	)
	return logger
}

func RandomString(length int) string {
	b := make([]byte, length+2)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[2 : length+2]
}

func RandomPortNumber() int {
	return 3500 + mathrand.Intn(100)
}

// Create a random port number between 3500 and 3600
func RandomPort() string {
	return ":" + strconv.Itoa(RandomPortNumber())
}

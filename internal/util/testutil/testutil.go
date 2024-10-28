package testutil

import (
	"crypto/rand"
	"fmt"
	mathrand "math/rand"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	internalch "github.com/hookdeck/EventKit/internal/clickhouse"
	internalredis "github.com/hookdeck/EventKit/internal/redis"
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

func CreateTestClickHouseConfig(t *testing.T) *internalch.ClickHouseConfig {
	return &internalch.ClickHouseConfig{
		Addr:     "127.0.0.1:9000",
		Username: "default",
		Password: "",
		Database: "default",
	}
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

// Create a random port number between 3500 and 3600
func RandomPort() string {
	randomPortNumber := 3500 + mathrand.Intn(100)
	return ":" + strconv.Itoa(randomPortNumber)
}

package testutil

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

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

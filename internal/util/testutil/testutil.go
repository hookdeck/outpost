package testutil

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
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

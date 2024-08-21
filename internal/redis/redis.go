package redis

import (
	"fmt"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	r "github.com/redis/go-redis/v9"
)

var (
	rdb  *r.Client
	once sync.Once
)

const (
	Nil = r.Nil
)

func Client() *r.Client {
	once.Do(func() {
		rdb = r.NewClient(&r.Options{
			Addr:     fmt.Sprintf("%s:%s", config.RedisHost, config.RedisPort),
			Password: config.RedisPassword,
			DB:       config.RedisDatabase,
		})
	})
	return rdb
}

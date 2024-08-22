package data

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
)

func Run(ctx context.Context) error {
	log.Println("running data service")

	if os.Getenv("DISABLED") == "true" {
		log.Println("data service is disabled")
		return nil
	}

	for range time.Tick(time.Second * 1) {
		keys, err := redis.Client().Keys(ctx, "destination:*").Result()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println(fmt.Sprintf("%d destination(s)", len(keys)))
	}

	return nil
}

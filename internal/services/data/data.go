package data

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
)

type DataService struct{}

func NewService(ctx context.Context, wg *sync.WaitGroup) *DataService {
	go func() {
		defer wg.Done()
		<-ctx.Done()
		log.Println("shutting down data service")
	}()
	return &DataService{}
}

func (s *DataService) Run(ctx context.Context) error {
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

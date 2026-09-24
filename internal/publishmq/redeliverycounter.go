package publishmq

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

// redeliveryCounterTTL is refreshed on every failure, so a count expires a day
// after the message last failed.
const redeliveryCounterTTL = 24 * time.Hour

// RedeliveryCounter counts failed receives per message.
type RedeliveryCounter interface {
	Incr(ctx context.Context, messageKey string) (int64, error)
}

type redisRedeliveryCounter struct {
	client       redis.Cmdable
	deploymentID string
}

func NewRedisRedeliveryCounter(client redis.Cmdable, deploymentID string) RedeliveryCounter {
	return &redisRedeliveryCounter{client: client, deploymentID: deploymentID}
}

func (c *redisRedeliveryCounter) Incr(ctx context.Context, messageKey string) (int64, error) {
	key := "publishmq:failures:" + messageKey
	if c.deploymentID != "" {
		key = c.deploymentID + ":" + key
	}
	var incr *redis.IntCmd
	_, err := c.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		incr = pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, redeliveryCounterTTL)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

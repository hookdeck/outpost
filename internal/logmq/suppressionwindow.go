package logmq

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

// redisSuppressionWindow is a SuppressionWindow over one Redis key per window:
// SETNX with the window as TTL claims it; a caller that fails to claim skips
// its send. The claim is the window — there is no separate in-flight state and
// no waiting on another claimant, because the window's contract is one send
// per key per window regardless of which caller wins. A failed send releases
// the claim so the next caller in the window sends instead.
type redisSuppressionWindow struct {
	client       redis.Client
	deploymentID string
	window       time.Duration
}

func NewRedisSuppressionWindow(client redis.Client, deploymentID string, window time.Duration) SuppressionWindow {
	return &redisSuppressionWindow{client: client, deploymentID: deploymentID, window: window}
}

func (w *redisSuppressionWindow) Exec(ctx context.Context, key string, exec func(context.Context) error) error {
	key = w.prefixKey(key)
	claimed, err := w.client.SetNX(ctx, key, "1", w.window).Result()
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if err := exec(ctx); err != nil {
		// Release even when exec failed because ctx expired, or the claim
		// suppresses every send until the window ends.
		if delErr := w.client.Del(context.WithoutCancel(ctx), key).Err(); delErr != nil {
			return errors.Join(err, delErr)
		}
		return err
	}
	return nil
}

func (w *redisSuppressionWindow) prefixKey(key string) string {
	if w.deploymentID == "" {
		return key
	}
	return w.deploymentID + ":" + key
}

package services

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// TopicSchemasHeartbeatWorker reports the topic schema configuration this API
// service applied as running (topicschema.Heartbeat). A deploy that restarts
// instances on it then isn't checked for breaking changes against a newer
// one, and once no instance reports it, a configuration rolled back to can
// replace it.
type TopicSchemasHeartbeatWorker struct {
	redis        redis.Cmdable
	deploymentID string
	hash         string
	interval     time.Duration
	ttl          time.Duration
	logger       *logging.Logger
}

// NewTopicSchemasHeartbeatWorker reports hash every interval for ttl.
func NewTopicSchemasHeartbeatWorker(rdb redis.Cmdable, deploymentID, hash string, interval, ttl time.Duration, logger *logging.Logger) *TopicSchemasHeartbeatWorker {
	return &TopicSchemasHeartbeatWorker{redis: rdb, deploymentID: deploymentID, hash: hash, interval: interval, ttl: ttl, logger: logger}
}

func (w *TopicSchemasHeartbeatWorker) Name() string {
	return "topic-schemas-heartbeat"
}

// Run reports the configuration at once, then every interval, until ctx is
// done. Failures are logged and retried at the next interval, so it always
// returns nil.
func (w *TopicSchemasHeartbeatWorker) Run(ctx context.Context) error {
	worker.Ready(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		w.beat(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *TopicSchemasHeartbeatWorker) beat(ctx context.Context) {
	bctx, cancel := context.WithTimeout(ctx, w.interval)
	defer cancel()
	if err := topicschema.Heartbeat(bctx, w.redis, w.deploymentID, w.hash, w.ttl); err != nil && ctx.Err() == nil {
		w.logger.Warn("failed to report the topic schema configuration as running", zap.Error(err))
	}
}

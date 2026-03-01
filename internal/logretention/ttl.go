package logretention

import (
	"context"
	"errors"
	"fmt"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/redis"
	"go.uber.org/zap"
)

// policyStore abstracts the persistence of applied TTL state.
type policyStore interface {
	GetAppliedTTL(ctx context.Context) (int, error)
	SetAppliedTTL(ctx context.Context, ttlDays int) error
}

// logStoreTTL abstracts TTL application to a log store.
type logStoreTTL interface {
	ApplyTTL(ctx context.Context, ttlDays int) error
}

// Apply compares the desired TTL with the persisted TTL.
// If they differ, it applies the TTL to ClickHouse and persists the new value to Redis.
func Apply(ctx context.Context, redisClient redis.Cmdable, chConn clickhouse.DB, deploymentID string, desiredTTLDays int, logger *logging.Logger) error {
	if desiredTTLDays < 0 {
		return errors.New("log retention TTL days must be >= 0")
	}

	ps := NewRedisPolicyStore(redisClient, deploymentID)
	ls := NewClickHouseTTL(chConn, deploymentID)

	return sync(ctx, ps, ls, desiredTTLDays, logger)
}

// sync compares the desired TTL with the persisted TTL and applies if different.
func sync(ctx context.Context, ps policyStore, ls logStoreTTL, desiredTTLDays int, logger *logging.Logger) error {
	persistedTTL, err := ps.GetAppliedTTL(ctx)
	if err != nil {
		return fmt.Errorf("failed to read persisted TTL: %w", err)
	}

	// No change needed: either TTL matches, or this is a fresh setup with no TTL configured.
	if persistedTTL == desiredTTLDays || (persistedTTL == -1 && desiredTTLDays == 0) {
		logger.Debug("TTL unchanged, skipping",
			zap.Int("ttl_days", desiredTTLDays))
		return nil
	}

	logger.Info("applying log retention TTL",
		zap.Int("old_ttl_days", persistedTTL),
		zap.Int("new_ttl_days", desiredTTLDays))

	if err := ls.ApplyTTL(ctx, desiredTTLDays); err != nil {
		return err
	}

	if err := ps.SetAppliedTTL(ctx, desiredTTLDays); err != nil {
		return fmt.Errorf("failed to persist TTL: %w", err)
	}

	logger.Info("log retention TTL applied successfully",
		zap.Int("ttl_days", desiredTTLDays))

	return nil
}

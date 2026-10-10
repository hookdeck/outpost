package redistenantstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

// Parked retries: retries of a disabled destination wait in a set beside it
// instead of being dropped. A write that enables the destination again moves
// the set to a one-shot resume set in the same script
// (UpdateDestinationIfLive with WithResumeParkedRetries), so a retry is either
// parked before that write, and resumed, or sees the destination enabled.

func (s *store) ParkRetry(ctx context.Context, tenantID, destinationID, member string, max int, expireAt time.Time) (driver.ParkResult, error) {
	if member == "" {
		return "", errors.New("park retry: empty member")
	}
	if max <= 0 {
		return "", errors.New("park retry: max must be positive")
	}
	if expireAt.IsZero() {
		return "", errors.New("park retry: missing expiry")
	}
	keys := []string{
		s.redisDestinationID(destinationID, tenantID),
		s.redisParkedRetriesKey(tenantID, destinationID),
	}
	status, err := parkRetryScript.Run(ctx, s.redisClient, keys, member, max, expireAt.UnixMilli()).Text()
	if err != nil {
		return "", err
	}
	switch result := driver.ParkResult(status); result {
	case driver.ParkResultParked, driver.ParkResultEnabled, driver.ParkResultGone, driver.ParkResultFull:
		return result, nil
	}
	return "", fmt.Errorf("unexpected park script reply %q", status)
}

func (s *store) PopResumeMembers(ctx context.Context, tenantID, key string, n int) ([]string, error) {
	if !s.validResumeKey(tenantID, key) {
		return nil, driver.ErrInvalidResumeKey
	}
	if n <= 0 {
		return nil, errors.New("pop resume members: n must be positive")
	}
	members, err := s.redisClient.SPopN(ctx, key, int64(n)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	return members, err
}

func (s *store) DeleteResumeSet(ctx context.Context, tenantID, key string) error {
	if !s.validResumeKey(tenantID, key) {
		return driver.ErrInvalidResumeKey
	}
	return s.redisClient.Del(ctx, key).Err()
}

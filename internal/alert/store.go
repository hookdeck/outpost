package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixAlert = "alert" // Base prefix for all alert keys
	keyFailures    = "cf"    // Set for consecutive failure attempt IDs
	alertKeyTTL    = 24 * time.Hour
)

// AlertStore persists the tracker's own state: the consecutive-failure streak
// per destination.
type AlertStore interface {
	// RecordConsecutiveFailure records a failed attempt and returns the
	// destination's current consecutive-failure count. Recording is idempotent
	// per attempt ID, so replays never double-count.
	//
	// Once the count reaches maxFailures, the first attempt to get there
	// becomes the streak's max-failure holder; isMaxHolder is true for that
	// attempt only, including its replays.
	RecordConsecutiveFailure(ctx context.Context, tenantID, destinationID, attemptID string, maxFailures int) (count int, isMaxHolder bool, err error)
	ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error
}

// recordFailureScript adds attempt ARGV[1] to the failure set KEYS[1],
// refreshes its TTL (ARGV[3] seconds), and returns {count, isMaxHolder}.
//
// The max-failure holder is stored in the same set as two marker members, so it
// shares the set's TTL and is cleared by the same reset that ends the streak:
//   - "~max" means the streak has a holder (one lookup, no scan),
//   - "~max:<attempt_id>" names it, so the holder's replay is recognized.
//
// The markers are not failures: the count is the set size minus 2 once they
// are present; a generated attempt ID can't equal either marker. The script
// runs atomically, so concurrent attempts past maxFailures (ARGV[2]) get
// exactly one holder.
var recordFailureScript = redis.NewScript(`
redis.call('SADD', KEYS[1], ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[3])
local held = redis.call('SISMEMBER', KEYS[1], '~max') == 1
local count = redis.call('SCARD', KEYS[1])
if held then count = count - 2 end
if count < tonumber(ARGV[2]) then return {count, 0} end
if not held then
	redis.call('SADD', KEYS[1], '~max', '~max:' .. ARGV[1])
	return {count, 1}
end
return {count, redis.call('SISMEMBER', KEYS[1], '~max:' .. ARGV[1])}
`)

type redisAlertStore struct {
	client       redis.Cmdable
	deploymentID string
}

// NewRedisAlertStore creates a new Redis-backed alert store
func NewRedisAlertStore(client redis.Cmdable, deploymentID string) AlertStore {
	return &redisAlertStore{
		client:       client,
		deploymentID: deploymentID,
	}
}

func (s *redisAlertStore) RecordConsecutiveFailure(ctx context.Context, tenantID, destinationID, attemptID string, maxFailures int) (int, bool, error) {
	key := s.getFailuresKey(tenantID, destinationID)

	res, err := recordFailureScript.Run(ctx, s.client, []string{key}, attemptID, maxFailures, int(alertKeyTTL.Seconds())).Int64Slice()
	if err != nil {
		return 0, false, fmt.Errorf("failed to record consecutive failure: %w", err)
	}
	if len(res) != 2 {
		return 0, false, fmt.Errorf("unexpected consecutive failure script result: %v", res)
	}

	return int(res[0]), res[1] == 1, nil
}

func (s *redisAlertStore) ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(tenantID, destinationID)).Err()
}

func (s *redisAlertStore) deploymentPrefix() string {
	if s.deploymentID == "" {
		return ""
	}
	return fmt.Sprintf("%s:", s.deploymentID)
}

func (s *redisAlertStore) getFailuresKey(tenantID, destinationID string) string {
	return fmt.Sprintf("%s%s:%s:%s:%s", s.deploymentPrefix(), keyPrefixAlert, tenantID, destinationID, keyFailures)
}

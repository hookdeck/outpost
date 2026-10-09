// Package deliverystatus keeps a small per-destination record of the latest
// delivery attempt in Redis: when the last attempt ran, how it ended, and when
// the last success was. The log pipeline writes it for MCP destinations as
// attempts are persisted, so the MCP subscribe handler can report a
// subscription's deliveryStatus with one HGETALL instead of querying the log
// store.
package deliverystatus

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/redis/go-redis/v9"
)

// DefaultTTL is the record lifetime when none is given: the longest MCP
// subscription TTL (24h) plus 7 days, so a record outlives any subscription
// that can still refresh.
const DefaultTTL = 8 * 24 * time.Hour

// maxCodeLength caps the stored attempt code. Codes come from our own
// publishers (an HTTP status or a fixed classification), so the cap only
// guards against an unexpected one.
const maxCodeLength = 64

// Hash fields.
const (
	fieldLastAttemptAt = "last_attempt_at"
	fieldLastSuccessAt = "last_success_at"
	fieldLastStatus    = "last_status"
	fieldLastCode      = "last_code"
)

// recordScript applies one destination's aggregated batch to its record.
// Attempts are persisted in no particular order (concurrent batches and
// replicas), so each field only moves forward in time.
//
// KEYS[1]: the record. ARGV: [1] latest attempt time (Unix ms), [2] its
// status, [3] its code, [4] latest success time (Unix ms, 0 = none in this
// batch), [5] TTL (s). Scores are compared with tonumber on both sides:
// Dragonfly's Lua returns numbers where Redis returns strings.
var recordScript = redis.NewScript(`
local at = tonumber(ARGV[1])
local cur = tonumber(redis.call('HGET', KEYS[1], 'last_attempt_at') or '')
if cur == nil or at >= cur then
  redis.call('HSET', KEYS[1], 'last_attempt_at', ARGV[1], 'last_status', ARGV[2], 'last_code', ARGV[3])
end
local s = tonumber(ARGV[4])
if s > 0 then
  local curS = tonumber(redis.call('HGET', KEYS[1], 'last_success_at') or '')
  if curS == nil or s > curS then
    redis.call('HSET', KEYS[1], 'last_success_at', ARGV[4])
  end
end
redis.call('EXPIRE', KEYS[1], ARGV[5])
return 1
`)

// Status is the latest delivery outcome recorded for one destination.
type Status struct {
	// LastAttemptAt is when the latest attempt ran.
	LastAttemptAt time.Time
	// LastStatus and LastCode describe the latest attempt
	// (models.AttemptStatusSuccess/Failed and its code).
	LastStatus string
	LastCode   string
	// LastSuccessAt is when the latest successful attempt ran; zero when none
	// was recorded.
	LastSuccessAt time.Time
}

// LastAttemptFailed reports whether the latest recorded attempt failed.
func (s *Status) LastAttemptFailed() bool {
	return s.LastStatus == models.AttemptStatusFailed
}

// RedisStore reads and writes status records at
// [<deployment>:]tenant:{<tenant>}:mcp_status:<destination>.
type RedisStore struct {
	client       redis.Cmdable
	deploymentID string
	ttlSeconds   int64
}

// NewRedisStore returns a store whose records expire ttl after their last
// write (DefaultTTL when ttl <= 0).
func NewRedisStore(client redis.Cmdable, deploymentID string, ttl time.Duration) *RedisStore {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &RedisStore{
		client:       client,
		deploymentID: deploymentID,
		ttlSeconds:   max(int64((ttl+time.Second-1)/time.Second), 1),
	}
}

func (s *RedisStore) key(tenantID, destinationID string) string {
	prefix := ""
	if s.deploymentID != "" {
		prefix = s.deploymentID + ":"
	}
	return fmt.Sprintf("%stenant:{%s}:mcp_status:%s", prefix, tenantID, destinationID)
}

// update is one destination's batch, aggregated.
type update struct {
	key         string
	attemptAtMs int64
	status      string
	code        string
	successAtMs int64
}

// RecordAttempts folds the entries into their destinations' records: one
// script call per destination, all pipelined in one round trip. Entries
// without an attempt, tenant, destination or time are skipped.
func (s *RedisStore) RecordAttempts(ctx context.Context, entries []*models.LogEntry) error {
	updates := make(map[string]*update)
	order := make([]string, 0)
	for _, entry := range entries {
		if entry == nil || entry.Attempt == nil || entry.Attempt.Time.IsZero() {
			continue
		}
		tenantID := entry.Attempt.TenantID
		if tenantID == "" && entry.Event != nil {
			tenantID = entry.Event.TenantID
		}
		destinationID := entry.Attempt.DestinationID
		if tenantID == "" || destinationID == "" {
			continue
		}

		key := s.key(tenantID, destinationID)
		u, ok := updates[key]
		if !ok {
			u = &update{key: key}
			updates[key] = u
			order = append(order, key)
		}
		at := entry.Attempt.Time.UnixMilli()
		if at >= u.attemptAtMs {
			u.attemptAtMs = at
			u.status = entry.Attempt.Status
			u.code = truncate(entry.Attempt.Code, maxCodeLength)
		}
		if entry.Attempt.Status == models.AttemptStatusSuccess && at > u.successAtMs {
			u.successAtMs = at
		}
	}
	if len(order) == 0 {
		return nil
	}

	run := func(eval func(pipe redis.Pipeliner, u *update)) error {
		_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for _, key := range order {
				eval(pipe, updates[key])
			}
			return nil
		})
		return err
	}
	err := run(func(pipe redis.Pipeliner, u *update) {
		recordScript.EvalSha(ctx, pipe, []string{u.key}, s.args(u)...)
	})
	if err != nil && redis.HasErrorPrefix(err, "NOSCRIPT") {
		// Script cache empty (restart, failover, new cluster node): send the
		// source, which also loads it for next time.
		err = run(func(pipe redis.Pipeliner, u *update) {
			recordScript.Eval(ctx, pipe, []string{u.key}, s.args(u)...)
		})
	}
	if err != nil {
		return fmt.Errorf("record attempt status: %w", err)
	}
	return nil
}

func (s *RedisStore) args(u *update) []any {
	return []any{u.attemptAtMs, u.status, u.code, u.successAtMs, s.ttlSeconds}
}

// GetAttemptStatus returns the destination's record, or nil when there is
// none (no recorded attempt, or it expired). Unparsable timestamps read as
// zero.
func (s *RedisStore) GetAttemptStatus(ctx context.Context, tenantID, destinationID string) (*Status, error) {
	fields, err := s.client.HGetAll(ctx, s.key(tenantID, destinationID)).Result()
	if err != nil {
		return nil, fmt.Errorf("get attempt status: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return &Status{
		LastAttemptAt: parseMillis(fields[fieldLastAttemptAt]),
		LastStatus:    fields[fieldLastStatus],
		LastCode:      fields[fieldLastCode],
		LastSuccessAt: parseMillis(fields[fieldLastSuccessAt]),
	}, nil
}

func parseMillis(v string) time.Time {
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

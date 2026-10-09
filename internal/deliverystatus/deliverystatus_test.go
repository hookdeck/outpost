package deliverystatus_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/deliverystatus"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T, deploymentID string, ttl time.Duration) (*deliverystatus.RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return deliverystatus.NewRedisStore(client, deploymentID, ttl), mr
}

var base = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func entry(tenantID, destID, status, code string, at time.Time) *models.LogEntry {
	return &models.LogEntry{
		Event: &models.Event{ID: "evt", TenantID: tenantID},
		Attempt: &models.Attempt{
			ID:              "att",
			TenantID:        tenantID,
			DestinationID:   destID,
			DestinationType: "mcp",
			Status:          status,
			Code:            code,
			Time:            at,
		},
	}
}

func get(t *testing.T, s *deliverystatus.RedisStore, tenantID, destID string) *deliverystatus.Status {
	t.Helper()
	status, err := s.GetAttemptStatus(context.Background(), tenantID, destID)
	require.NoError(t, err)
	return status
}

func TestRecordAttempts_Content(t *testing.T) {
	t.Parallel()
	s, mr := newStore(t, "dep", 48*time.Hour)
	ctx := context.Background()

	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base),
		entry("t1", "sub_1", models.AttemptStatusFailed, "503", base.Add(time.Second)),
	}))

	key := "dep:tenant:{t1}:mcp_status:sub_1"
	require.True(t, mr.Exists(key))
	assert.Equal(t, strconvMs(base.Add(time.Second)), mr.HGet(key, "last_attempt_at"))
	assert.Equal(t, "failed", mr.HGet(key, "last_status"))
	assert.Equal(t, "503", mr.HGet(key, "last_code"))
	assert.Equal(t, strconvMs(base), mr.HGet(key, "last_success_at"))
	assert.Equal(t, 48*time.Hour, mr.TTL(key))

	status := get(t, s, "t1", "sub_1")
	require.NotNil(t, status)
	assert.Equal(t, deliverystatus.Status{
		LastAttemptAt: base.Add(time.Second),
		LastStatus:    models.AttemptStatusFailed,
		LastCode:      "503",
		LastSuccessAt: base,
	}, *status)
	assert.True(t, status.LastAttemptFailed())
}

func TestRecordAttempts_KeyWithoutDeploymentPrefix(t *testing.T) {
	t.Parallel()
	s, mr := newStore(t, "", time.Hour)
	require.NoError(t, s.RecordAttempts(context.Background(), []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base),
	}))
	assert.Equal(t, []string{"tenant:{t1}:mcp_status:sub_1"}, mr.Keys())
}

// Attempts are persisted out of order across batches and replicas: each field
// only moves forward in time.
func TestRecordAttempts_OutOfOrder(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, "", time.Hour)
	ctx := context.Background()

	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusFailed, "503", base.Add(2*time.Second)),
	}))
	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base.Add(time.Second)),
	}))
	status := get(t, s, "t1", "sub_1")
	assert.Equal(t, base.Add(2*time.Second), status.LastAttemptAt, "an older attempt does not overwrite the latest")
	assert.Equal(t, models.AttemptStatusFailed, status.LastStatus)
	assert.Equal(t, base.Add(time.Second), status.LastSuccessAt, "but its success is still the latest success")

	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base),
	}))
	assert.Equal(t, base.Add(time.Second), get(t, s, "t1", "sub_1").LastSuccessAt, "an older success never moves it back")
}

// Timestamps compare as numbers, not strings ("999" > "1000" as strings).
func TestRecordAttempts_NumericComparison(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, "", time.Hour)
	ctx := context.Background()
	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusFailed, "500", time.UnixMilli(999)),
	}))
	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", time.UnixMilli(1000)),
	}))
	status := get(t, s, "t1", "sub_1")
	assert.Equal(t, time.UnixMilli(1000).UTC(), status.LastAttemptAt)
	assert.Equal(t, models.AttemptStatusSuccess, status.LastStatus)
}

// One batch aggregates per destination, in any order.
func TestRecordAttempts_BatchAggregation(t *testing.T) {
	t.Parallel()
	s, mr := newStore(t, "", time.Hour)
	require.NoError(t, s.RecordAttempts(context.Background(), []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusFailed, "503", base.Add(3*time.Second)),
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base.Add(time.Second)),
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base.Add(2*time.Second)),
		entry("t2", "sub_1", models.AttemptStatusSuccess, "202", base),
	}))
	assert.Len(t, mr.Keys(), 2, "records are per tenant and destination")

	s1 := get(t, s, "t1", "sub_1")
	assert.Equal(t, base.Add(3*time.Second), s1.LastAttemptAt)
	assert.Equal(t, "503", s1.LastCode)
	assert.Equal(t, base.Add(2*time.Second), s1.LastSuccessAt)

	s2 := get(t, s, "t2", "sub_1")
	assert.Equal(t, "202", s2.LastCode)
	assert.Equal(t, base, s2.LastSuccessAt)
}

func TestRecordAttempts_TTLRefreshedOnWrite(t *testing.T) {
	t.Parallel()
	s, mr := newStore(t, "", 10*time.Minute)
	ctx := context.Background()
	key := "tenant:{t1}:mcp_status:sub_1"

	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base)}))
	mr.FastForward(9 * time.Minute)
	assert.Equal(t, time.Minute, mr.TTL(key))

	// Even a write that changes nothing (older attempt) refreshes the TTL.
	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{entry("t1", "sub_1", models.AttemptStatusFailed, "500", base.Add(-time.Hour))}))
	assert.Equal(t, 10*time.Minute, mr.TTL(key))

	mr.FastForward(11 * time.Minute)
	assert.Nil(t, get(t, s, "t1", "sub_1"), "an expired record reads as absent")
}

func TestNewRedisStore_TTL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ttl  time.Duration
		want time.Duration
	}{
		{0, deliverystatus.DefaultTTL},
		{-time.Second, deliverystatus.DefaultTTL},
		{1500 * time.Millisecond, 2 * time.Second},
		{time.Millisecond, time.Second},
	} {
		s, mr := newStore(t, "", tc.ttl)
		require.NoError(t, s.RecordAttempts(context.Background(), []*models.LogEntry{entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base)}))
		assert.Equal(t, tc.want, mr.TTL("tenant:{t1}:mcp_status:sub_1"), "ttl %s", tc.ttl)
	}
}

// Invalid entries are skipped; the tenant falls back to the event's.
func TestRecordAttempts_SkipsIncompleteEntries(t *testing.T) {
	t.Parallel()
	s, mr := newStore(t, "", time.Hour)

	noTenant := entry("", "sub_2", models.AttemptStatusSuccess, "200", base)
	noTenant.Event.TenantID = "t_event"

	require.NoError(t, s.RecordAttempts(context.Background(), []*models.LogEntry{
		nil,
		{Event: &models.Event{ID: "evt"}},
		entry("t1", "sub_1", models.AttemptStatusSuccess, "200", time.Time{}),
		entry("t1", "", models.AttemptStatusSuccess, "200", base),
		noTenant,
	}))
	assert.Equal(t, []string{"tenant:{t_event}:mcp_status:sub_2"}, mr.Keys())

	require.NoError(t, s.RecordAttempts(context.Background(), nil), "empty batch is a no-op")
}

func TestRecordAttempts_CodeIsCapped(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, "", time.Hour)
	long := strings.Repeat("x", 63) + "é" + strings.Repeat("y", 100)
	require.NoError(t, s.RecordAttempts(context.Background(), []*models.LogEntry{
		entry("t1", "sub_1", models.AttemptStatusFailed, long, base),
	}))
	code := get(t, s, "t1", "sub_1").LastCode
	assert.LessOrEqual(t, len(code), 64)
	assert.Equal(t, strings.Repeat("x", 63), code, "a rune cut at the cap is dropped, not split")
}

// The script is sent with EVALSHA; an empty script cache (restart, failover)
// falls back to EVAL.
func TestRecordAttempts_ScriptCacheFlushed(t *testing.T) {
	t.Parallel()
	s, mr := newStore(t, "", time.Hour)
	ctx := context.Background()
	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{entry("t1", "sub_1", models.AttemptStatusFailed, "500", base)}))

	mr.FlushAll()
	admin := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer admin.Close()
	require.NoError(t, admin.ScriptFlush(ctx).Err())

	require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{entry("t1", "sub_1", models.AttemptStatusSuccess, "200", base.Add(time.Second))}))
	assert.Equal(t, models.AttemptStatusSuccess, get(t, s, "t1", "sub_1").LastStatus)
}

func TestGetAttemptStatus(t *testing.T) {
	t.Parallel()
	s, mr := newStore(t, "dep", time.Hour)

	assert.Nil(t, get(t, s, "t1", "missing"))

	key := "dep:tenant:{t1}:mcp_status:sub_1"
	mr.HSet(key, "last_attempt_at", "garbage")
	mr.HSet(key, "last_status", "failed")
	mr.HSet(key, "last_code", "timeout")
	status := get(t, s, "t1", "sub_1")
	require.NotNil(t, status)
	assert.True(t, status.LastAttemptAt.IsZero(), "unparsable timestamps read as zero")
	assert.True(t, status.LastSuccessAt.IsZero())
	assert.Equal(t, "timeout", status.LastCode)

	// Another tenant never sees this record.
	assert.Nil(t, get(t, s, "t2", "sub_1"))
}

func strconvMs(t time.Time) string {
	return strconv.FormatInt(t.UnixMilli(), 10)
}

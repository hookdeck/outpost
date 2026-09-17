package logmq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisSuppressionWindow_SecondCallerSkips(t *testing.T) {
	t.Parallel()
	w := logmq.NewRedisSuppressionWindow(testutil.CreateTestRedisClient(t), "dep", time.Minute)
	ctx := context.Background()

	calls := 0
	exec := func(context.Context) error { calls++; return nil }
	require.NoError(t, w.Exec(ctx, "k", exec))
	require.NoError(t, w.Exec(ctx, "k", exec))
	assert.Equal(t, 1, calls)
}

func TestRedisSuppressionWindow_KeysAreIndependent(t *testing.T) {
	t.Parallel()
	w := logmq.NewRedisSuppressionWindow(testutil.CreateTestRedisClient(t), "", time.Minute)
	ctx := context.Background()

	calls := 0
	exec := func(context.Context) error { calls++; return nil }
	require.NoError(t, w.Exec(ctx, "a", exec))
	require.NoError(t, w.Exec(ctx, "b", exec))
	assert.Equal(t, 2, calls)
}

// A caller that lands while another holds the claim returns immediately: the
// window never waits on the in-flight send.
func TestRedisSuppressionWindow_InFlightCallerSkipsWithoutWaiting(t *testing.T) {
	t.Parallel()
	w := logmq.NewRedisSuppressionWindow(testutil.CreateTestRedisClient(t), "", time.Minute)
	ctx := context.Background()

	claimed := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- w.Exec(ctx, "k", func(context.Context) error {
			close(claimed)
			<-release
			return nil
		})
	}()
	<-claimed

	start := time.Now()
	require.NoError(t, w.Exec(ctx, "k", func(context.Context) error { t.Error("contender must not exec"); return nil }))
	assert.Less(t, time.Since(start), 100*time.Millisecond)

	close(release)
	require.NoError(t, <-holderDone)
}

func TestRedisSuppressionWindow_FailedExecReleasesClaim(t *testing.T) {
	t.Parallel()
	w := logmq.NewRedisSuppressionWindow(testutil.CreateTestRedisClient(t), "", time.Minute)
	ctx := context.Background()

	boom := errors.New("boom")
	err := w.Exec(ctx, "k", func(context.Context) error { return boom })
	require.ErrorIs(t, err, boom)

	calls := 0
	require.NoError(t, w.Exec(ctx, "k", func(context.Context) error { calls++; return nil }))
	assert.Equal(t, 1, calls, "claim released on failure, next caller sends")
}

// The claim is released even when exec failed because ctx expired: the
// release must not run on the dead ctx.
func TestRedisSuppressionWindow_ExpiredCtxStillReleasesClaim(t *testing.T) {
	t.Parallel()
	w := logmq.NewRedisSuppressionWindow(testutil.CreateTestRedisClient(t), "", time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	err := w.Exec(ctx, "k", func(ctx context.Context) error { cancel(); return ctx.Err() })
	require.ErrorIs(t, err, context.Canceled)

	calls := 0
	require.NoError(t, w.Exec(context.Background(), "k", func(context.Context) error { calls++; return nil }))
	assert.Equal(t, 1, calls)
}

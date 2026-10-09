package idempotence_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// miniredis TTLs only move on FastForward, so claim lifetimes are asserted
// exactly and deterministically.
func newMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return mr, client
}

// holdClaim runs an Exec on key that blocks until release is closed, and
// waits until it holds the processing claim.
func holdClaim(t *testing.T, i idempotence.Idempotence, key string, release <-chan struct{}) <-chan error {
	t.Helper()
	claimed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- i.Exec(context.Background(), key, func(context.Context) error {
			close(claimed)
			<-release
			return nil
		})
	}()
	select {
	case <-claimed:
	case <-time.After(2 * time.Second):
		t.Fatal("first Exec never claimed the key")
	}
	return done
}

func TestIdempotence_ProcessingTTL(t *testing.T) {
	t.Parallel()

	t.Run("claim TTL is set separately from the duplicate wait", func(t *testing.T) {
		t.Parallel()
		mr, client := newMiniredis(t)
		i := idempotence.New(client,
			idempotence.WithTimeout(50*time.Millisecond),
			idempotence.WithProcessingTTL(15*time.Second),
		)
		release := make(chan struct{})
		done := holdClaim(t, i, "k", release)

		assert.Equal(t, 15*time.Second, mr.TTL("k"), "processing claim lives for the processing TTL")

		// A duplicate waits only the (short) wait, then conflicts: the claim is
		// still held.
		start := time.Now()
		err := i.Exec(context.Background(), "k", func(context.Context) error {
			t.Error("duplicate must not run while the claim is held")
			return nil
		})
		assert.ErrorIs(t, err, idempotence.ErrConflict)
		assert.Less(t, time.Since(start), time.Second)

		// Past the wait but within the processing TTL the claim still holds.
		mr.FastForward(10 * time.Second)
		assert.True(t, mr.Exists("k"))

		close(release)
		require.NoError(t, <-done)
	})

	t.Run("claim TTL defaults to the timeout", func(t *testing.T) {
		t.Parallel()
		mr, client := newMiniredis(t)
		i := idempotence.New(client, idempotence.WithTimeout(3*time.Second))
		release := make(chan struct{})
		done := holdClaim(t, i, "k", release)
		assert.Equal(t, 3*time.Second, mr.TTL("k"))
		close(release)
		require.NoError(t, <-done)
	})

	t.Run("an expired claim lets the next caller run", func(t *testing.T) {
		t.Parallel()
		mr, client := newMiniredis(t)
		i := idempotence.New(client,
			idempotence.WithTimeout(50*time.Millisecond),
			idempotence.WithProcessingTTL(15*time.Second),
		)
		release := make(chan struct{})
		defer close(release)
		holdClaim(t, i, "k", release)

		mr.FastForward(16 * time.Second)
		ran := false
		require.NoError(t, i.Exec(context.Background(), "k", func(context.Context) error {
			ran = true
			return nil
		}))
		assert.True(t, ran)
	})
}

func TestIdempotence_DuplicateWaitHonoursContext(t *testing.T) {
	t.Parallel()
	_, client := newMiniredis(t)
	i := idempotence.New(client, idempotence.WithTimeout(time.Minute))
	release := make(chan struct{})
	defer close(release)
	holdClaim(t, i, "k", release)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	err := i.Exec(ctx, "k", func(context.Context) error {
		t.Error("duplicate must not run")
		return nil
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 5*time.Second, "the wait returns on ctx cancel, not after the full timeout")
}

func TestIdempotence_FailedExecClearsClaimOnCanceledContext(t *testing.T) {
	t.Parallel()
	mr, client := newMiniredis(t)
	i := idempotence.New(client, idempotence.WithTimeout(time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	err := i.Exec(ctx, "k", func(ctx context.Context) error {
		cancel() // e.g. shutdown mid-attempt
		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, mr.Exists("k"), "the claim is released even though ctx was canceled")

	ran := false
	require.NoError(t, i.Exec(context.Background(), "k", func(context.Context) error {
		ran = true
		return nil
	}))
	assert.True(t, ran, "a redelivery runs immediately instead of hitting ErrConflict")
}

func TestIdempotence_FailedExecReturnsExecError(t *testing.T) {
	t.Parallel()
	_, client := newMiniredis(t)
	i := idempotence.New(client)
	boom := errors.New("boom")
	assert.ErrorIs(t, i.Exec(context.Background(), "k", func(context.Context) error { return boom }), boom)
}

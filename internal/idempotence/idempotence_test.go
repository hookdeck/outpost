package idempotence_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/idempotence"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func setupCountExec(_ *testing.T, ctx context.Context, timeout time.Duration, ex func() error) (exec func() error, countexec func(count *int), cleanup func()) {
	execchan := make(chan struct{})
	exec = func() error {
		time.Sleep(timeout)
		execchan <- struct{}{}
		return ex()
	}
	cleanup = func() {
		close(execchan)
	}
	countexec = func(count *int) {
		for {
			select {
			case <-execchan:
				*count++
			case <-ctx.Done():
				return
			}
		}
	}
	return exec, countexec, cleanup
}

func TestIdempotence_Success(t *testing.T) {
	t.Parallel()

	i := idempotence.New(testutil.CreateTestRedisClient(t),
		idempotence.WithTimeout(3*time.Second),
		idempotence.WithSuccessfulTTL(24*time.Hour),
	)

	t.Run("on separate keys", func(t *testing.T) {
		t.Parallel()
		// Arange
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 0, func() error { return nil })
		defer cleanup()
		// Act
		go func() {
			i.Exec(ctx, "1", exec) // 1st exec
		}()
		go func() {
			i.Exec(ctx, "2", exec) // 2nd exec
		}()
		// Assert
		count := 0
		go countexec(&count)
		<-ctx.Done()
		assert.Equal(t, 2, count, "should execute twice")
	})

	t.Run("when 2nd exec is within processing window", func(t *testing.T) {
		t.Parallel()
		// Arange
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 1*time.Second, func() error { return nil })
		defer cleanup()
		// Act
		key := testutil.RandomString(5)
		go func() {
			i.Exec(ctx, key, exec) // 1st exec
		}()
		errchan := make(chan error)
		go func() {
			time.Sleep(time.Second / 2)
			errchan <- i.Exec(ctx, key, exec) // 2nd exec
		}()
		// Assert
		count := 0
		go countexec(&count)
		<-ctx.Done()
		err := <-errchan
		assert.Nil(t, err, "should not return error")
		assert.Equal(t, 1, count, "should execute once")
	})

	t.Run("when 2nd exec is after processed", func(t *testing.T) {
		t.Parallel()
		// Arange
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 1*time.Second, func() error { return nil })
		defer cleanup()
		// Act
		key := testutil.RandomString(5)
		go func() {
			i.Exec(ctx, key, exec) // 1st exec
		}()
		errchan := make(chan error)
		go func() {
			time.Sleep(2 * time.Second)       // wait for 1st exec to finish
			errchan <- i.Exec(ctx, key, exec) // 2nd exec
		}()
		// Assert
		count := 0
		go countexec(&count)
		<-ctx.Done()
		err := <-errchan
		assert.Nil(t, err, "should not return error")
		assert.Equal(t, 1, count, "should execute once")
	})
}

func TestIdempotence_Failure(t *testing.T) {
	t.Parallel()

	errExec := errors.New("exec error")

	i := idempotence.New(testutil.CreateTestRedisClient(t),
		idempotence.WithTimeout(3*time.Second),
		idempotence.WithSuccessfulTTL(24*time.Hour),
	)

	t.Run("when 2nd exec is within processing window", func(t *testing.T) {
		t.Parallel()
		// Arrange
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 1*time.Second, func() error { return errExec })
		defer cleanup()

		// Act
		key := testutil.RandomString(5)
		err1chan := make(chan error)
		err2chan := make(chan error)
		go func() {
			err1chan <- i.Exec(ctx, key, exec) // 1st exec
		}()
		go func() {
			time.Sleep(time.Second / 2)        // wait to make sure 1st exec has started
			err2chan <- i.Exec(ctx, key, exec) // 2nd exec
		}()

		// Assert
		count := 0
		go countexec(&count)
		<-ctx.Done()
		err1 := <-err1chan
		err2 := <-err2chan
		assert.Equal(t, errExec, err1, "first execution should return exec error")
		assert.Equal(t, idempotence.ErrConflict, err2, "second execution should return conflict error")
		assert.Equal(t, 1, count, "should execute once")
	})

	t.Run("when 2nd exec is after 1st exec completion", func(t *testing.T) {
		t.Parallel()
		// Arange
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 1*time.Second, func() error { return errExec })
		defer cleanup()
		// Act
		key := testutil.RandomString(5)
		err1chan := make(chan error)
		err2chan := make(chan error)
		go func() {
			err1chan <- i.Exec(ctx, key, exec) // 1st exec
		}()
		go func() {
			time.Sleep(2 * time.Second)        // wait for 1st exec to finish
			err2chan <- i.Exec(ctx, key, exec) // 2nd exec
		}()
		// Assert
		count := 0
		go countexec(&count)
		<-ctx.Done()
		err1 := <-err1chan
		err2 := <-err2chan
		assert.Equal(t, errExec, err1, "first execution should return exec error")
		assert.Equal(t, errExec, err2, "second execution should return exec error")
		assert.Equal(t, 2, count, "should execute twice")
	})
}

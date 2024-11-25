package idempotence_test

import (
	"context"
	"errors"
	"log"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCountExec(_ *testing.T, ctx context.Context, timeout time.Duration, ex func() error) (exec func(context.Context) error, countexec func(count *int), cleanup func()) {
	execchan := make(chan struct{})
	exec = func(_ context.Context) error {
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

// Setup:
// - 1 subscriber, 1 publisher
// - 1 message, failed twice before success
// The 2 failed execution won't ack or nack the message to test visibility timeout.
func TestIntegrationIdempotence_WithUnackedFailures(t *testing.T) {
	t.Parallel()

	visibilityTimeout := 5 * time.Second
	mq := startAWSSQSQueueWithVisibilityTimeout(context.Background(), t, visibilityTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), visibilityTimeout*3-visibilityTimeout/2)
	defer cancel()

	subscription, err := mq.Subscribe(ctx)
	require.Nil(t, err)

	msgs := []MockMsg{}
	go func() {
		defer subscription.Shutdown(ctx)
		i := idempotence.New(testutil.CreateTestRedisClient(t),
			idempotence.WithTimeout(3*time.Second),
		)

		for {
			log.Println("listening for messages...")
			msg, err := subscription.Receive(ctx)
			if err != nil {
				log.Println("subscription err", err)
				break
			}
			mockMsg := MockMsg{}
			mockMsg.FromMessage(msg)
			err = i.Exec(ctx, mockMsg.ID, func(context.Context) error {
				msgs = append(msgs, mockMsg)
				if len(msgs) < 3 {
					log.Println("exec: failed")
					return errors.New("failed") // return err so idempotency clears
				}
				log.Println("exec: success")
				return nil
			})
			if err == nil {
				log.Println("ack")
				msg.Ack()
			}
		}
	}()

	id := uuid.New().String()
	err = mq.Publish(ctx, &MockMsg{ID: id})

	require.Nil(t, err)

	<-ctx.Done()

	assert.Len(t, msgs, 3)
}

// Setup:
// - 2 subscriber, 1 publisher
// - message will sleep for 2s before success
// - publisher will publish the same message twice to test idempotency
func TestIntegrationIdempotence_WithConcurrentHandlerAndSuccess(t *testing.T) {
	t.Parallel()

	visibilityTimeout := 5 * time.Second
	mq := startAWSSQSQueueWithVisibilityTimeout(context.Background(), t, visibilityTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // exec should only take 2-4s
	defer cancel()

	subscription, err := mq.Subscribe(ctx)
	require.Nil(t, err)
	defer subscription.Shutdown(ctx)

	i := idempotence.New(testutil.CreateTestRedisClient(t),
		idempotence.WithTimeout(3*time.Second),
	)

	errchan := make(chan error)
	execTimestamps := []struct {
		Start time.Time
		End   time.Time
	}{}
	consumerFn := func(name string) {
		for {
			log.Printf("%s: listening for messages...", name)
			msg, err := subscription.Receive(ctx)
			if err != nil {
				log.Printf("%s: subscription err: %s", name, err)
				break
			}
			mockMsg := MockMsg{}
			mockMsg.FromMessage(msg)
			err = i.Exec(ctx, mockMsg.ID, func(context.Context) error {
				start := time.Now()
				time.Sleep(2 * time.Second)
				end := time.Now()
				execTimestamps = append(execTimestamps, struct {
					Start time.Time
					End   time.Time
				}{Start: start, End: end})
				return nil
			})
			errchan <- err
			if err == nil {
				log.Printf("%s: ack", name)
				msg.Ack()
			} else {
				log.Printf("%s: nack", name)
				msg.Nack()
			}
		}
	}

	go consumerFn("1")
	go consumerFn("2")
	errs := []error{}
	go func() {
		for {
			select {
			case err := <-errchan:
				errs = append(errs, err)
			case <-ctx.Done():
				return
			}
		}
	}()

	id := uuid.New().String()
	err = mq.Publish(ctx, &MockMsg{ID: id})
	require.Nil(t, err)
	err = mq.Publish(ctx, &MockMsg{ID: id})
	require.Nil(t, err)

	<-ctx.Done()

	assert.Len(t, execTimestamps, 1)
	require.Len(t, errs, 2, "should have 2 errors")
	assert.True(t, errs[0] == nil && errs[1] == nil, "both consumers should succeed")
}

// Setup:
// - 2 subscriber, 1 publisher
// - message will failed twice, each exec taking 2s
// - publisher will publish the same message twice to test idempotency
func TestIntegrationIdempotence_WithConcurrentHandlerAndFailedExecution(t *testing.T) {
	t.Parallel()

	visibilityTimeout := 5 * time.Second
	mq := startAWSSQSQueueWithVisibilityTimeout(context.Background(), t, visibilityTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), visibilityTimeout*3-visibilityTimeout/2)
	defer cancel()

	subscription, err := mq.Subscribe(ctx)
	require.Nil(t, err)
	defer subscription.Shutdown(ctx)

	i := idempotence.New(testutil.CreateTestRedisClient(t),
		idempotence.WithTimeout(3*time.Second),
	)

	execTimestamps := []struct {
		Start time.Time
		End   time.Time
	}{}
	consumerFn := func(name string) {
		for {
			log.Printf("%s: listening for messages...", name)
			msg, err := subscription.Receive(ctx)
			if err != nil {
				log.Printf("%s: subscription err: %s", name, err)
				break
			}
			mockMsg := MockMsg{}
			mockMsg.FromMessage(msg)
			err = i.Exec(ctx, mockMsg.ID, func(context.Context) error {
				start := time.Now()
				time.Sleep(2 * time.Second)
				end := time.Now()
				execTimestamps = append(execTimestamps, struct {
					Start time.Time
					End   time.Time
				}{Start: start, End: end})
				if len(execTimestamps) < 3 {
					log.Printf("%s: exec: failed", name)
					return errors.New("failed") // return err so idempotency clears
				}
				log.Printf("%s: exec: success", name)
				return nil
			})
			if err == nil {
				log.Printf("%s: ack", name)
				msg.Ack()
			} else {
				log.Printf("%s: nack", name)
				msg.Nack()
			}
		}
	}

	go consumerFn("1")
	go consumerFn("2")

	id := uuid.New().String()
	err = mq.Publish(ctx, &MockMsg{ID: id})
	require.Nil(t, err)
	err = mq.Publish(ctx, &MockMsg{ID: id})
	require.Nil(t, err)

	<-ctx.Done()

	require.Len(t, execTimestamps, 3)
	prevEnd := execTimestamps[0].End
	for i, timestamp := range execTimestamps {
		if i == 0 {
			continue
		}
		require.Less(t, prevEnd, timestamp.Start, "executions should not overlap")
	}
}

type MockMsg struct {
	ID string
}

var _ mqs.IncomingMessage = &MockMsg{}

func (m *MockMsg) FromMessage(msg *mqs.Message) error {
	m.ID = string(msg.Body)
	return nil
}

func (m *MockMsg) ToMessage() (*mqs.Message, error) {
	return &mqs.Message{Body: []byte(m.ID)}, nil
}

func startAWSSQSQueueWithVisibilityTimeout(ctx context.Context, t *testing.T, visibilityTimeout time.Duration) mqs.Queue {
	t.Cleanup(testinfra.Start(t))
	mqConfig := testinfra.NewMQAWSConfig(t, map[string]string{
		"VisibilityTimeout": strconv.Itoa(int(visibilityTimeout.Seconds())),
	})
	mq := mqs.NewQueue(&mqConfig)
	cleanup, err := mq.Init(ctx)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	return mq
}

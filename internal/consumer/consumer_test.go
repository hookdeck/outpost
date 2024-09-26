package consumer_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type execTimestamps struct {
	Start time.Time
	End   time.Time
}

type consumerTest struct {
	ctx          context.Context
	mq           mqs.Queue
	makeConsumer func(consumer.MessageHandler, mqs.Subscription) consumer.Consumer
	act          func(*testing.T, context.Context)
	assert       func(*testing.T, context.Context, []execTimestamps, error)
}

func (c *consumerTest) run(t *testing.T) {
	cleanup, _ := c.mq.Init(c.ctx)
	defer cleanup()
	subscription, _ := c.mq.Subscribe(c.ctx)

	consumerExecchan := make(chan []execTimestamps)
	execchan := make(chan execTimestamps)

	handler := struct{ handlerImpl }{}
	handler.handle = func(ctx context.Context, msg *mqs.Message) error {
		start := time.Now()
		time.Sleep(1 * time.Second)
		message := &Message{}
		if err := message.FromMessage(msg); err != nil {
			msg.Nack()
			return err
		}
		log.Println(message.ID)
		msg.Ack()
		end := time.Now()
		execchan <- execTimestamps{Start: start, End: end}
		return nil
	}

	go func() {
		execs := []execTimestamps{}
		for {
			select {
			case exec := <-execchan:
				execs = append(execs, exec)
			case <-c.ctx.Done():
				consumerExecchan <- execs
			}
		}
	}()

	csm := c.makeConsumer(&handler, subscription)
	errchan := make(chan error)
	go func() {
		errchan <- csm.Run(c.ctx)
	}()

	c.act(t, c.ctx)

	var err error
	select {
	case err = <-errchan:
	case <-c.ctx.Done():
	}

	c.assert(t, c.ctx, <-consumerExecchan, err)
}

func TestConsumer_SingleHandler(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mq := mqs.NewQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}})

	test := consumerTest{
		ctx: ctx,
		mq:  mq,
		makeConsumer: func(handler consumer.MessageHandler, subscription mqs.Subscription) consumer.Consumer {
			return consumer.New(subscription, handler, consumer.WithConcurrency(1))
		},
		act: func(t *testing.T, ctx context.Context) {
			mq.Publish(ctx, &Message{ID: "1"})
			mq.Publish(ctx, &Message{ID: "2"})
			mq.Publish(ctx, &Message{ID: "3"})
		},
		assert: func(t *testing.T, ctx context.Context, execs []execTimestamps, err error) {
			require.Nil(t, err)
			require.Len(t, execs, 3)
			var timestamp time.Time
			for i, exec := range execs {
				if i == 0 {
					timestamp = exec.End
					continue
				}
				require.True(t, exec.Start.After(timestamp), "messages should be handled sequentially")
			}
		},
	}

	test.run(t)
}

func TestConsumer_ConcurrentHandler(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mq := mqs.NewQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}})

	test := consumerTest{
		ctx: ctx,
		mq:  mq,
		makeConsumer: func(handler consumer.MessageHandler, subscription mqs.Subscription) consumer.Consumer {
			return consumer.New(subscription, handler, consumer.WithConcurrency(2))
		},
		act: func(t *testing.T, ctx context.Context) {
			mq.Publish(ctx, &Message{ID: "1"})
			mq.Publish(ctx, &Message{ID: "2"})
			mq.Publish(ctx, &Message{ID: "3"})
			mq.Publish(ctx, &Message{ID: "4"})
			mq.Publish(ctx, &Message{ID: "5"})
		},
		assert: func(t *testing.T, ctx context.Context, execs []execTimestamps, err error) {
			require.Nil(t, err)
			require.Len(t, execs, 5)
			assert.True(t,
				execs[0].Start.Before(execs[1].End) && execs[1].Start.Before(execs[0].End),
				"2 first messages should be handled in parallel",
			)
			assert.True(t,
				execs[2].Start.After(execs[0].End) || execs[2].Start.After(execs[1].End),
				"the 3rd message should be handled after the 1st and 2nd messages",
			)
			assert.True(t,
				execs[2].Start.Before(execs[3].End) && execs[3].Start.Before(execs[2].End),
				"the 3rd and 4th message should be handled in parallel",
			)
			assert.True(t,
				execs[4].Start.After(execs[2].End) || execs[4].Start.After(execs[3].End),
				"the 5th message should be handled after the 3rd and 4th messages",
			)
		},
	}

	test.run(t)
}

// ==================================== Mock ====================================

type Message struct {
	ID string
}

var _ mqs.IncomingMessage = &Message{}

func (m *Message) ToMessage() (*mqs.Message, error) {
	return &mqs.Message{Body: []byte(m.ID)}, nil
}

func (m *Message) FromMessage(msg *mqs.Message) error {
	m.ID = string(msg.Body)
	return nil
}

type handlerImpl struct {
	handle func(context.Context, *mqs.Message) error
}

var _ consumer.MessageHandler = &handlerImpl{}

func (h *handlerImpl) Handle(ctx context.Context, msg *mqs.Message) error {
	return h.handle(ctx, msg)
}

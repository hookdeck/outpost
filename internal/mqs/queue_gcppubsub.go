package mqs

import (
	"context"
	"fmt"
	"sync"
	"time"

	nativepubsub "cloud.google.com/go/pubsub"
	"gocloud.dev/gcp"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/gcppubsub"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

type GCPPubSubConfig struct {
	ProjectID                 string
	TopicID                   string
	SubscriptionID            string
	ServiceAccountCredentials string // JSON key file content
}

type GCPPubSubQueue struct {
	once              *sync.Once
	base              *wrappedBaseQueue
	config            *GCPPubSubConfig
	visibilityTimeout time.Duration
	topic             *pubsub.Topic
	cleanupFns        []func()
}

var _ Queue = &GCPPubSubQueue{}

func NewGCPPubSubQueue(config *GCPPubSubConfig, visibilityTimeout time.Duration) *GCPPubSubQueue {
	var once sync.Once
	return &GCPPubSubQueue{
		config:            config,
		visibilityTimeout: visibilityTimeout,
		once:              &once,
		base:              newWrappedBaseQueue(),
		cleanupFns:        []func(){},
	}
}

func (q *GCPPubSubQueue) Init(ctx context.Context) (func(), error) {
	var err error
	q.once.Do(func() {
		err = q.initTopic(ctx)
	})
	if err != nil {
		return nil, err
	}
	return func() {
		for _, fn := range q.cleanupFns {
			fn()
		}
	}, nil
}

func (q *GCPPubSubQueue) getConn(ctx context.Context) (*grpc.ClientConn, error) {
	credentials, err := google.CredentialsFromJSON(ctx, []byte(q.config.ServiceAccountCredentials), "https://www.googleapis.com/auth/pubsub")
	if err != nil {
		return nil, err
	}
	ts := gcp.CredentialsTokenSource(credentials)

	conn, cleanup, err := gcppubsub.Dial(ctx, ts)
	if err != nil {
		return nil, err
	}
	q.cleanupFns = append(q.cleanupFns, cleanup)
	return conn, nil
}

func (q *GCPPubSubQueue) initTopic(ctx context.Context) error {
	if q.config.ServiceAccountCredentials != "" {
		return q.initTopicWithCredentials(ctx)
	}
	return q.initTopicWithoutCredentials(ctx)
}

func (q *GCPPubSubQueue) initTopicWithCredentials(ctx context.Context) error {
	conn, err := q.getConn(ctx)
	if err != nil {
		return err
	}

	pubClient, err := gcppubsub.PublisherClient(ctx, conn)
	if err != nil {
		return err
	}
	q.cleanupFns = append(q.cleanupFns, func() {
		pubClient.Close()
	})

	topic, err := gcppubsub.OpenTopicByPath(pubClient,
		fmt.Sprintf("projects/%s/topics/%s", q.config.ProjectID, q.config.TopicID),
		nil)
	if err != nil {
		return err
	}
	q.topic = topic
	q.cleanupFns = append(q.cleanupFns, func() {
		q.topic.Shutdown(ctx)
	})
	return nil
}

func (q *GCPPubSubQueue) initTopicWithoutCredentials(ctx context.Context) error {
	topic, err := pubsub.OpenTopic(ctx,
		fmt.Sprintf("gcppubsub://projects/%s/topics/%s", q.config.ProjectID, q.config.TopicID))
	if err != nil {
		return err
	}
	q.topic = topic
	q.cleanupFns = append(q.cleanupFns, func() {
		q.topic.Shutdown(ctx)
	})
	return nil
}

func (q *GCPPubSubQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	return q.base.Publish(ctx, q.topic, incomingMessage, nil)
}

func (q *GCPPubSubQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	o := ApplySubscribeOptions(opts)
	concurrency := o.Concurrency

	var clientOpts []option.ClientOption
	if q.config.ServiceAccountCredentials != "" {
		creds, err := google.CredentialsFromJSON(ctx, []byte(q.config.ServiceAccountCredentials), "https://www.googleapis.com/auth/pubsub")
		if err != nil {
			return nil, fmt.Errorf("parse credentials: %w", err)
		}
		clientOpts = append(clientOpts, option.WithCredentials(creds))
	}

	client, err := nativepubsub.NewClient(ctx, q.config.ProjectID, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create pubsub client: %w", err)
	}

	sub := client.Subscription(q.config.SubscriptionID)
	sub.ReceiveSettings.MaxOutstandingMessages = concurrency
	// Use a single StreamingPull stream per subscription to keep concurrency
	// control explicit; scaling is done at the subscription level, not via
	// additional goroutines within a subscription.
	sub.ReceiveSettings.NumGoroutines = 1
	// Disable automatic lease extension so messages are not held beyond the
	// subscription's ack deadline. We are intentional about consumer processing
	// logic and do not want the SDK silently extending message leases — if a
	// handler exceeds the ack deadline, the message should be redelivered.
	sub.ReceiveSettings.MaxExtension = -1 * time.Second
	// The native SDK sends a "receipt modack" (ModifyAckDeadline) when it first
	// receives a message, using its internal ack-latency p99 as the deadline
	// (minimum 10s). This overrides the subscription's ackDeadlineSeconds on the
	// server side. Without MinExtensionPeriod, a subscription configured with a
	// 60s ack deadline effectively becomes 10s, causing premature redelivery for
	// any handler that takes >10s. Setting MinExtensionPeriod to match the
	// subscription's visibility timeout prevents this override.
	sub.ReceiveSettings.MinExtensionPeriod = q.visibilityTimeout

	msgChan := make(chan *Message, concurrency)
	subCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	s := &gcpNativeSubscription{
		msgChan: msgChan,
		cancel:  cancel,
		done:    done,
		client:  client,
	}

	go func() {
		defer close(done)
		defer close(msgChan)
		// sub.Receive blocks until subCtx is cancelled or a fatal error occurs.
		// The callback nacks on context cancellation to avoid buffering messages
		// that won't be processed.
		s.recvErr = sub.Receive(subCtx, func(_ context.Context, msg *nativepubsub.Message) {
			m := &Message{
				QueueMessage: &gcpNativeAcker{msg: msg},
				LoggableID:   msg.ID,
				Body:         msg.Data,
			}
			select {
			case msgChan <- m:
			case <-subCtx.Done():
				msg.Nack()
			}
		})
	}()

	return s, nil
}

// gcpNativeSubscription bridges the native SDK StreamingPull to the mqs.Subscription interface.
type gcpNativeSubscription struct {
	msgChan <-chan *Message
	cancel  context.CancelFunc
	done    chan struct{}
	client  *nativepubsub.Client
	recvErr error // set by the background goroutine when sub.Receive exits
}

var _ Subscription = &gcpNativeSubscription{}
var _ ConcurrentSubscription = &gcpNativeSubscription{}

func (s *gcpNativeSubscription) Receive(ctx context.Context) (*Message, error) {
	select {
	case msg, ok := <-s.msgChan:
		if !ok {
			if s.recvErr != nil {
				return nil, fmt.Errorf("subscription closed: %w", s.recvErr)
			}
			return nil, fmt.Errorf("subscription closed")
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *gcpNativeSubscription) Shutdown(_ context.Context) error {
	s.cancel()
	<-s.done
	// Nack any remaining buffered messages for faster redelivery.
	for msg := range s.msgChan {
		msg.Nack()
	}
	return s.client.Close()
}

// SupportsConcurrency returns true — the native SDK manages concurrency via
// MaxOutstandingMessages, so the consumer should skip its own semaphore.
func (s *gcpNativeSubscription) SupportsConcurrency() bool {
	return true
}

// gcpNativeAcker wraps a native SDK message to implement QueueMessage.
type gcpNativeAcker struct {
	msg *nativepubsub.Message
}

func (a *gcpNativeAcker) Ack()  { a.msg.Ack() }
func (a *gcpNativeAcker) Nack() { a.msg.Nack() }

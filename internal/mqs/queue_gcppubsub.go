package mqs

import (
	"context"
	"fmt"
	"sync"

	"gocloud.dev/gcp"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/gcppubsub"
	"golang.org/x/oauth2/google"
	"google.golang.org/grpc"
)

type GCPPubSubQueue struct {
	once       *sync.Once
	base       *wrappedBaseQueue
	config     *GCPPubSubConfig
	topic      *pubsub.Topic
	cleanupFns []func()
}

var _ Queue = &GCPPubSubQueue{}

func NewGCPPubSubQueue(config *GCPPubSubConfig) *GCPPubSubQueue {
	var once sync.Once
	return &GCPPubSubQueue{
		config:     config,
		once:       &once,
		base:       newWrappedBaseQueue(),
		cleanupFns: []func(){},
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

func (q *GCPPubSubQueue) Subscribe(ctx context.Context) (Subscription, error) {
	var err error
	var subscription *pubsub.Subscription
	if q.config.ServiceAccountCredentials != "" {
		subscription, err = q.createSubscriptionWithCredentials(ctx)
	} else {
		subscription, err = q.createSubscriptionWithoutCredentials(ctx)
	}
	if err != nil {
		return nil, err
	}
	return q.base.Subscribe(ctx, subscription)
}

func (q *GCPPubSubQueue) createSubscriptionWithCredentials(ctx context.Context) (*pubsub.Subscription, error) {
	conn, err := q.getConn(ctx)
	if err != nil {
		return nil, err
	}

	subClient, err := gcppubsub.SubscriberClient(ctx, conn)
	if err != nil {
		return nil, err
	}
	q.cleanupFns = append(q.cleanupFns, func() {
		subClient.Close()
	})

	subscription := gcppubsub.OpenSubscription(subClient, gcp.ProjectID(q.config.ProjectID), q.config.SubscriptionID, nil)
	return subscription, nil
}

func (q *GCPPubSubQueue) createSubscriptionWithoutCredentials(ctx context.Context) (*pubsub.Subscription, error) {
	subscription, err := pubsub.OpenSubscription(ctx,
		fmt.Sprintf("gcppubsub://projects/%s/subscriptions/%s", q.config.ProjectID, q.config.SubscriptionID))
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

package mqinfra_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const retryLimit = 5

type Config struct {
	infra mqinfra.MQInfraConfig
	mq    mqs.QueueConfig
}

func testMQInfra(t *testing.T, mqConfig *Config, dlqConfig *Config) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	t.Run("should create queue", func(t *testing.T) {
		ctx := context.Background()
		infra := mqinfra.New(&mqConfig.infra)
		exists, err := infra.Exist(ctx)
		require.NoError(t, err)
		if !exists {
			require.NoError(t, infra.Declare(ctx))
			exists, err = infra.Exist(ctx)
			require.NoError(t, err)
			require.True(t, exists)
		}

		t.Cleanup(func() {
			require.NoError(t, infra.TearDown(ctx))
		})

		mq := mqs.NewQueue(&mqConfig.mq)
		cleanup, err := mq.Init(ctx)
		require.NoError(t, err)
		t.Cleanup(cleanup)
		subscription, err := mq.Subscribe(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			subscription.Shutdown(ctx)
		})
		msgchan := make(chan *testutil.MockMsg)
		go func() {
			for {
				msg, err := subscription.Receive(ctx)
				if err != nil {
					log.Println(err)
					return
				}
				log.Println("Received message", msg)
				msg.Ack()
				mockMsg := &testutil.MockMsg{}
				if err := mockMsg.FromMessage(msg); err != nil {
					log.Println("Error parsing message", err)
				} else {
					msgchan <- mockMsg
				}
			}
		}()

		msg := &testutil.MockMsg{ID: idgen.String()}
		require.NoError(t, mq.Publish(ctx, msg))

		var receivedMsg *testutil.MockMsg
		select {
		case receivedMsg = <-msgchan:
		case <-time.After(1 * time.Second):
			require.Fail(t, "timeout waiting for message")
		}

		assert.Equal(t, msg.ID, receivedMsg.ID)
	})

	// Assertion:
	// - When the message is nacked, it should be retried 5 times before being sent to the DLQ.
	// - Afterwards, reading the DLQ should return the message.
	t.Run("should create dlq queue", func(t *testing.T) {
		ctx := context.Background()
		infra := mqinfra.New(&mqConfig.infra)
		exists, err := infra.Exist(ctx)
		require.NoError(t, err)
		if !exists {
			require.NoError(t, infra.Declare(ctx))
			exists, err = infra.Exist(ctx)
			require.NoError(t, err)
			require.True(t, exists)
		}

		t.Cleanup(func() {
			require.NoError(t, infra.TearDown(ctx))
		})

		mq := mqs.NewQueue(&mqConfig.mq)
		cleanup, err := mq.Init(ctx)
		require.NoError(t, err)
		t.Cleanup(cleanup)
		subscription, err := mq.Subscribe(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			subscription.Shutdown(ctx)
		})
		msgchan := make(chan *testutil.MockMsg)
		go func() {
			for {
				msg, err := subscription.Receive(ctx)
				if err != nil {
					log.Println(err)
					return
				}
				log.Println("Received message", msg)
				msg.Nack()
				mockMsg := &testutil.MockMsg{}
				if err := mockMsg.FromMessage(msg); err != nil {
					log.Println("Error parsing message", err)
				} else {
					msgchan <- mockMsg
				}
			}
		}()

		msg := &testutil.MockMsg{ID: idgen.String()}
		require.NoError(t, mq.Publish(ctx, msg))

		msgCount := 0
		expectedCount := retryLimit + 1
		timeout := time.After(10 * time.Second) // Safety timeout
	loop:
		for msgCount < expectedCount {
			select {
			case <-msgchan:
				msgCount++
			case <-timeout:
				break loop
			}
		}
		require.Equal(t, expectedCount, msgCount)

		dlmq := mqs.NewQueue(&dlqConfig.mq)
		dlsubscription, err := dlmq.Subscribe(ctx)
		require.NoError(t, err)
		dlmsgchan := make(chan *testutil.MockMsg)
		go func() {
			for {
				msg, err := dlsubscription.Receive(ctx)
				if err != nil {
					log.Println(err)
					return
				}
				log.Println("Received message", msg)
				msg.Ack()
				mockMsg := &testutil.MockMsg{}
				if err := mockMsg.FromMessage(msg); err != nil {
					log.Println("Error parsing message", err)
				} else {
					dlmsgchan <- mockMsg
				}
			}
		}()
		var dlmsg *testutil.MockMsg
		dlTimeout := time.After(10 * time.Second) // Safety timeout
		select {
		case dlmsg = <-dlmsgchan:
			// Got the DLQ message
		case <-dlTimeout:
			require.Fail(t, "timeout waiting for DLQ message")
		}
		require.NotNil(t, dlmsg, "should receive DLQ message")
		assert.Equal(t, msg.ID, dlmsg.ID)
	})
}

func TestIntegrationMQInfra_RabbitMQ(t *testing.T) {
	exchange := idgen.String()
	queue := idgen.String()

	testMQInfra(t,
		&Config{
			infra: mqinfra.MQInfraConfig{
				RabbitMQ: &mqinfra.RabbitMQInfraConfig{
					ServerURL: testinfra.EnsureRabbitMQ(),
					Exchange:  exchange,
					Queue:     queue,
				},
				Policy: mqinfra.Policy{
					RetryLimit: retryLimit,
				},
			},
			mq: mqs.QueueConfig{
				RabbitMQ: &mqs.RabbitMQConfig{
					ServerURL: testinfra.EnsureRabbitMQ(),
					Exchange:  exchange,
					Queue:     queue,
				},
			},
		},
		&Config{
			infra: mqinfra.MQInfraConfig{
				RabbitMQ: &mqinfra.RabbitMQInfraConfig{
					ServerURL: testinfra.EnsureRabbitMQ(),
					Exchange:  exchange,
					Queue:     queue + ".dlq",
				},
			},
			mq: mqs.QueueConfig{
				RabbitMQ: &mqs.RabbitMQConfig{
					ServerURL: testinfra.EnsureRabbitMQ(),
					Exchange:  exchange,
					Queue:     queue + ".dlq",
				},
			},
		},
	)
}

func TestIntegrationMQInfra_AWSSQS(t *testing.T) {
	q := idgen.String()

	testMQInfra(t,
		&Config{
			infra: mqinfra.MQInfraConfig{
				AWSSQS: &mqinfra.AWSSQSInfraConfig{
					Endpoint:                  testinfra.EnsureLocalStack(),
					ServiceAccountCredentials: "test:test:",
					Region:                    "us-east-1",
					Topic:                     q,
				},
				Policy: mqinfra.Policy{
					RetryLimit:        retryLimit,
					VisibilityTimeout: 1,
				},
			},
			mq: mqs.QueueConfig{
				AWSSQS: &mqs.AWSSQSConfig{
					Endpoint:                  testinfra.EnsureLocalStack(),
					ServiceAccountCredentials: "test:test:",
					Region:                    "us-east-1",
					Topic:                     q,
					WaitTime:                  1 * time.Second,
				},
			},
		},
		&Config{
			infra: mqinfra.MQInfraConfig{
				AWSSQS: &mqinfra.AWSSQSInfraConfig{
					Endpoint:                  testinfra.EnsureLocalStack(),
					ServiceAccountCredentials: "test:test:",
					Region:                    "us-east-1",
					Topic:                     q + "-dlq",
				},
			},
			mq: mqs.QueueConfig{
				AWSSQS: &mqs.AWSSQSConfig{
					Endpoint:                  testinfra.EnsureLocalStack(),
					ServiceAccountCredentials: "test:test:",
					Region:                    "us-east-1",
					Topic:                     q + "-dlq",
					WaitTime:                  1 * time.Second,
				},
			},
		},
	)
}

func TestIntegrationMQInfra_GCPPubSub(t *testing.T) {
	// Set PUBSUB_EMULATOR_HOST environment variable
	testinfra.EnsureGCP()

	topicID := "test-" + idgen.String()
	subscriptionID := topicID + "-subscription"

	testMQInfra(t,
		&Config{
			infra: mqinfra.MQInfraConfig{
				GCPPubSub: &mqinfra.GCPPubSubInfraConfig{
					ProjectID:                 "test-project",
					TopicID:                   topicID,
					SubscriptionID:            subscriptionID,
					ServiceAccountCredentials: "",
					MinRetryBackoff:           1,
					MaxRetryBackoff:           1,
				},
				Policy: mqinfra.Policy{
					RetryLimit:        retryLimit,
					VisibilityTimeout: 10,
				},
			},
			mq: mqs.QueueConfig{
				GCPPubSub: &mqs.GCPPubSubConfig{
					ProjectID:                 "test-project",
					TopicID:                   topicID,
					SubscriptionID:            subscriptionID,
					ServiceAccountCredentials: "",
				},
			},
		},
		&Config{
			infra: mqinfra.MQInfraConfig{
				GCPPubSub: &mqinfra.GCPPubSubInfraConfig{
					ProjectID:                 "test-project",
					TopicID:                   topicID + "-dlq",
					SubscriptionID:            topicID + "-dlq-sub",
					ServiceAccountCredentials: "",
				},
			},
			mq: mqs.QueueConfig{
				GCPPubSub: &mqs.GCPPubSubConfig{
					ProjectID:                 "test-project",
					TopicID:                   topicID + "-dlq",
					SubscriptionID:            topicID + "-dlq-sub",
					ServiceAccountCredentials: "",
				},
			},
		},
	)
}

func TestIntegrationMQInfra_AzureServiceBus(t *testing.T) {
	t.Skip("skip TestIntegrationMQInfra_AzureServiceBus integration test for now since the emulator doesn't support managing resources")

	topic := idgen.String()
	subscription := topic + "-subscription"

	const (
		tenantID       = ""
		clientID       = ""
		clientSecret   = ""
		subscriptionID = ""
		resourceGroup  = ""
		namespace      = ""
	)

	testMQInfra(t,
		&Config{
			infra: mqinfra.MQInfraConfig{
				AzureServiceBus: &mqinfra.AzureServiceBusInfraConfig{
					TenantID:       tenantID,
					ClientID:       clientID,
					ClientSecret:   clientSecret,
					SubscriptionID: subscriptionID,
					ResourceGroup:  resourceGroup,
					Namespace:      namespace,
					Topic:          topic,
					Subscription:   subscription,
				},
				Policy: mqinfra.Policy{
					RetryLimit:        retryLimit,
					VisibilityTimeout: 10,
				},
			},
			mq: mqs.QueueConfig{
				AzureServiceBus: &mqs.AzureServiceBusConfig{
					TenantID:       tenantID,
					ClientID:       clientID,
					ClientSecret:   clientSecret,
					SubscriptionID: subscriptionID,
					ResourceGroup:  resourceGroup,
					Namespace:      namespace,
					Topic:          topic,
					Subscription:   subscription,
				},
			},
		},
		&Config{
			infra: mqinfra.MQInfraConfig{
				AzureServiceBus: &mqinfra.AzureServiceBusInfraConfig{
					TenantID:       tenantID,
					ClientID:       clientID,
					ClientSecret:   clientSecret,
					SubscriptionID: subscriptionID,
					ResourceGroup:  resourceGroup,
					Namespace:      namespace,
					Topic:          topic,        // Same topic as main queue
					Subscription:   subscription, // Same subscription as main queue
				}},
			mq: mqs.QueueConfig{
				AzureServiceBus: &mqs.AzureServiceBusConfig{
					TenantID:       tenantID,
					ClientID:       clientID,
					ClientSecret:   clientSecret,
					SubscriptionID: subscriptionID,
					ResourceGroup:  resourceGroup,
					Namespace:      namespace,
					Topic:          topic,        // Same topic as main queue
					Subscription:   subscription, // Same subscription as main queue
					DLQ:            true,         // Enable DLQ mode
				}},
		},
	)
}

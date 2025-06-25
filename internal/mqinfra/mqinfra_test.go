package mqinfra_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
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

		msg := &testutil.MockMsg{ID: uuid.New().String()}
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

		msg := &testutil.MockMsg{ID: uuid.New().String()}
		require.NoError(t, mq.Publish(ctx, msg))

		msgCount := 0
	loop:
		for {
			select {
			case <-msgchan:
				msgCount++
			case <-time.After(3 * time.Second):
				break loop
			}
		}
		require.Equal(t, retryLimit+1, msgCount)

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
		dlmsgCount := 0
	dlloop:
		for {
			select {
			case dlmsg = <-dlmsgchan:
				dlmsgCount++
			case <-time.After(1 * time.Second):
				break dlloop
			}
		}
		assert.Equal(t, 1, dlmsgCount)
		assert.Equal(t, msg.ID, dlmsg.ID)
	})
}

func TestIntegrationMQInfra_RabbitMQ(t *testing.T) {
	exchange := uuid.New().String()
	queue := uuid.New().String()

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
	q := uuid.New().String()

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
				},
			},
		},
	)
}

func TestIntegrationMQInfra_GCPPubSub(t *testing.T) {
	// Set PUBSUB_EMULATOR_HOST environment variable
	testinfra.EnsureGCP()

	topicID := "test-" + uuid.New().String()
	subscriptionID := topicID + "-subscription"

	testMQInfra(t,
		&Config{
			infra: mqinfra.MQInfraConfig{
				GCPPubSub: &mqinfra.GCPPubSubInfraConfig{
					ProjectID:                 "test-project",
					TopicID:                   topicID,
					SubscriptionID:            subscriptionID,
					ServiceAccountCredentials: "",
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

	topic := uuid.New().String()
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

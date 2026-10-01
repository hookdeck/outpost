package destawseventbridge_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawseventbridge"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const testSource = "outpost.test"

// eventBridgeEvent is the envelope EventBridge delivers to a rule target.
type eventBridgeEvent struct {
	ID         string          `json:"id"`
	DetailType string          `json:"detail-type"`
	Source     string          `json:"source"`
	Time       time.Time       `json:"time"`
	Detail     json.RawMessage `json:"detail"`
}

// SQSTargetConsumer reads the events a bus rule forwards to an SQS queue.
type SQSTargetConsumer struct {
	client   *sqs.Client
	queueURL string
	msgChan  chan testsuite.Message
	done     chan struct{}
	wg       sync.WaitGroup
}

func NewSQSTargetConsumer(client *sqs.Client, queueURL string) *SQSTargetConsumer {
	c := &SQSTargetConsumer{
		client:   client,
		queueURL: queueURL,
		msgChan:  make(chan testsuite.Message, 100),
		done:     make(chan struct{}),
	}
	c.wg.Add(1)
	go c.consume()
	return c
}

func (c *SQSTargetConsumer) consume() {
	defer c.wg.Done()
	for {
		select {
		case <-c.done:
			return
		default:
		}
		out, err := c.client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     1,
		})
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, m := range out.Messages {
			_, _ = c.client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(c.queueURL),
				ReceiptHandle: m.ReceiptHandle,
			})

			var ev eventBridgeEvent
			if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &ev); err != nil {
				fmt.Printf("Error unmarshaling EventBridge event: %v\n", err)
				continue
			}
			var detail struct {
				Metadata map[string]string `json:"metadata"`
				Data     json.RawMessage   `json:"data"`
			}
			if err := json.Unmarshal(ev.Detail, &detail); err != nil {
				fmt.Printf("Error unmarshaling detail: %v\n", err)
				continue
			}
			select {
			case c.msgChan <- testsuite.Message{Data: detail.Data, Metadata: detail.Metadata, Raw: ev}:
			case <-c.done:
				return
			}
		}
	}
}

func (c *SQSTargetConsumer) Consume() <-chan testsuite.Message {
	return c.msgChan
}

func (c *SQSTargetConsumer) Close() error {
	close(c.done)
	c.wg.Wait()
	close(c.msgChan)
	return nil
}

type EventBridgeAsserter struct{}

func (a *EventBridgeAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	ev, ok := msg.Raw.(eventBridgeEvent)
	if !ok {
		t.Errorf("unexpected raw message type %T", msg.Raw)
		return
	}
	assert.Equal(t, testSource, ev.Source, "source should be the provider-level source")
	assert.Equal(t, event.Topic, ev.DetailType, "detail-type should be the event topic")
	testsuite.AssertTimestampIsISO8601(t, msg.Metadata["timestamp"])
}

type eventBridgeFixture struct {
	eb       *eventbridge.Client
	sqs      *sqs.Client
	endpoint string
}

func newEventBridgeFixture(t *testing.T) *eventBridgeFixture {
	endpoint := testinfra.EnsureLocalStack()
	awsConfig, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)
	return &eventBridgeFixture{
		eb: eventbridge.NewFromConfig(awsConfig, func(o *eventbridge.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		}),
		sqs: sqs.NewFromConfig(awsConfig, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		}),
		endpoint: endpoint,
	}
}

// routeToQueue adds a rule to the bus that forwards every event from source to
// a fresh SQS queue, and returns the queue URL. An empty busName is the default
// bus; any other bus is created.
func (f *eventBridgeFixture) routeToQueue(t *testing.T, busName, source string) string {
	ctx := context.Background()
	name := "test-" + idgen.String()

	q, err := f.sqs.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name)})
	require.NoError(t, err)
	attrs, err := f.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       q.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	require.NoError(t, err)

	var bus *string
	if busName != "" {
		bus = aws.String(busName)
		_, err = f.eb.CreateEventBus(ctx, &eventbridge.CreateEventBusInput{Name: bus})
		require.NoError(t, err)
	}
	_, err = f.eb.PutRule(ctx, &eventbridge.PutRuleInput{
		Name:         aws.String(name),
		EventBusName: bus,
		EventPattern: aws.String(fmt.Sprintf(`{"source":[%q]}`, source)),
	})
	require.NoError(t, err)
	_, err = f.eb.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule:         aws.String(name),
		EventBusName: bus,
		Targets:      []ebtypes.Target{{Id: aws.String("sqs"), Arn: aws.String(attrs.Attributes["QueueArn"])}},
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = f.eb.RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{Rule: aws.String(name), EventBusName: bus, Ids: []string{"sqs"}})
		_, _ = f.eb.DeleteRule(ctx, &eventbridge.DeleteRuleInput{Name: aws.String(name), EventBusName: bus})
		if bus != nil {
			_, _ = f.eb.DeleteEventBus(ctx, &eventbridge.DeleteEventBusInput{Name: bus})
		}
		_, _ = f.sqs.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: q.QueueUrl})
	})
	return aws.ToString(q.QueueUrl)
}

type AWSEventBridgeSuite struct {
	testsuite.PublisherSuite
	fixture  *eventBridgeFixture
	provider destregistry.Provider
	consumer *SQSTargetConsumer
}

func TestAWSEventBridgeSuite(t *testing.T) {
	suite.Run(t, new(AWSEventBridgeSuite))
}

func (s *AWSEventBridgeSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))
	s.fixture = newEventBridgeFixture(t)

	var err error
	s.provider, err = destawseventbridge.New(testutil.Registry.MetadataLoader(), nil, destawseventbridge.WithSource(testSource))
	require.NoError(t, err)
}

func (s *AWSEventBridgeSuite) SetupTest() {
	t := s.T()
	busName := "test-" + idgen.String()
	s.consumer = NewSQSTargetConsumer(s.fixture.sqs, s.fixture.routeToQueue(t, busName, testSource))

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_eventbridge"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"endpoint":       s.fixture.endpoint,
			"event_bus_name": busName,
			"region":         "us-east-1",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":    "test",
			"secret": "test",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: s.provider,
		Dest:     &destination,
		Consumer: s.consumer,
		Asserter: &EventBridgeAsserter{},
	})
	s.PublisherSuite.SetupTest()
}

func (s *AWSEventBridgeSuite) TearDownTest() {
	s.PublisherSuite.TearDownTest()
	if s.consumer != nil {
		s.consumer.Close()
		s.consumer = nil
	}
}

// publishAndReceive publishes one event through a destination with the given
// bus and returns what the bus rule forwarded.
func publishAndReceive(t *testing.T, busName string, event models.Event) eventBridgeEvent {
	t.Helper()
	f := newEventBridgeFixture(t)
	source := "outpost.test." + idgen.String()
	consumer := NewSQSTargetConsumer(f.sqs, f.routeToQueue(t, busName, source))
	defer consumer.Close()

	provider, err := destawseventbridge.New(testutil.Registry.MetadataLoader(), nil, destawseventbridge.WithSource(source))
	require.NoError(t, err)
	config := map[string]string{"endpoint": f.endpoint, "region": "us-east-1"}
	if busName != "" {
		config["event_bus_name"] = busName
	}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_eventbridge"),
		testutil.DestinationFactory.WithConfig(config),
		testutil.DestinationFactory.WithCredentials(map[string]string{"key": "test", "secret": "test"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	delivery, err := publisher.Publish(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "success", delivery.Status)
	assert.NotEmpty(t, delivery.Response["event_id"])

	select {
	case msg := <-consumer.Consume():
		received := msg.Raw.(eventBridgeEvent)
		assert.Equal(t, source, received.Source)
		return received
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for the event on the rule target")
		return eventBridgeEvent{}
	}
}

func TestAWSEventBridgePublish_DefaultBus(t *testing.T) {
	t.Cleanup(testinfra.Start(t))

	event := testutil.EventFactory.Any(testutil.EventFactory.WithTopic("user.created"))
	received := publishAndReceive(t, "", event)

	assert.Equal(t, "user.created", received.DetailType)
}

func TestAWSEventBridgePublish_NoTopic(t *testing.T) {
	t.Cleanup(testinfra.Start(t))

	event := testutil.EventFactory.Any(testutil.EventFactory.WithTopic(""))
	received := publishAndReceive(t, "test-"+idgen.String(), event)

	assert.Equal(t, destawseventbridge.DefaultDetailType, received.DetailType)
}

func TestAWSEventBridgePublish_EventTime(t *testing.T) {
	t.Cleanup(testinfra.Start(t))

	eventTime := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithTime(eventTime),
	)
	received := publishAndReceive(t, "test-"+idgen.String(), event)

	assert.True(t, eventTime.Equal(received.Time), "expected %s, got %s", eventTime, received.Time)
}

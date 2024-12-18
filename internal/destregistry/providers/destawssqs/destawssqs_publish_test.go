package destawssqs_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/util/awsutil"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// SQSConsumer implements testsuite.MessageConsumer
type SQSConsumer struct {
	client   *sqs.Client
	queueURL string
	msgChan  chan testsuite.Message
	done     chan struct{}
}

func NewSQSConsumer(client *sqs.Client, queueURL string) *SQSConsumer {
	c := &SQSConsumer{
		client:   client,
		queueURL: queueURL,
		msgChan:  make(chan testsuite.Message),
		done:     make(chan struct{}),
	}
	go c.consume()
	return c
}

func (c *SQSConsumer) consume() {
	for {
		select {
		case <-c.done:
			return
		default:
			result, err := c.client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
				QueueUrl:              aws.String(c.queueURL),
				MaxNumberOfMessages:   1,
				WaitTimeSeconds:       5,
				MessageAttributeNames: []string{"All"},
			})
			if err != nil {
				continue
			}

			for _, msg := range result.Messages {
				metadata := make(map[string]string)
				for k, v := range msg.MessageAttributes {
					metadata[k] = *v.StringValue
				}

				c.msgChan <- testsuite.Message{
					Data:     []byte(*msg.Body),
					Metadata: metadata,
					Raw:      msg,
				}

				// Delete the message after processing
				_, _ = c.client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
					QueueUrl:      aws.String(c.queueURL),
					ReceiptHandle: msg.ReceiptHandle,
				})
			}
		}
	}
}

func (c *SQSConsumer) Consume() <-chan testsuite.Message {
	return c.msgChan
}

func (c *SQSConsumer) Close() error {
	close(c.done)
	return nil
}

type AWSSQSSuite struct {
	testsuite.PublisherSuite
	consumer *SQSConsumer
}

func TestAWSSQSSuite(t *testing.T) {
	suite.Run(t, new(AWSSQSSuite))
}

func (s *AWSSQSSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))
	mqConfig := testinfra.NewMQAWSConfig(t, nil)

	// Setup AWS config and client
	sqsClient, err := awsutil.SQSClientFromConfig(context.Background(), mqConfig.AWSSQS)
	require.NoError(t, err)
	queueURL, err := awsutil.EnsureQueue(context.Background(), sqsClient, mqConfig.AWSSQS.Topic, nil)
	require.NoError(t, err)

	// Create consumer
	s.consumer = NewSQSConsumer(sqsClient, queueURL)

	// Create provider
	provider, err := destawssqs.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	// Create destination
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_sqs"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"endpoint":  mqConfig.AWSSQS.Endpoint,
			"queue_url": queueURL,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test",
			"secret":  "test",
			"session": "",
		}),
	)

	// Initialize publisher suite
	cfg := testsuite.Config{
		Provider: provider,
		Dest:     &destination,
		Consumer: s.consumer,
	}
	s.InitSuite(cfg)
}

func (s *AWSSQSSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
}

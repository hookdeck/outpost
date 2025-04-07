package destawskinesis_test

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
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// KinesisConsumer implements testsuite.MessageConsumer
type KinesisConsumer struct {
	client       *kinesis.Client
	streamName   string
	shardId      string
	msgChan      chan testsuite.Message
	done         chan struct{}
	shuttingDown bool
	wg           sync.WaitGroup
}

// NewKinesisConsumer creates a new Kinesis consumer
func NewKinesisConsumer(client *kinesis.Client, streamName string) (*KinesisConsumer, error) {
	// Get shard ID for the stream
	describeOutput, err := client.DescribeStream(context.Background(), &kinesis.DescribeStreamInput{
		StreamName: aws.String(streamName),
	})
	if err != nil {
		return nil, err
	}

	// Get the first shard ID
	if len(describeOutput.StreamDescription.Shards) == 0 {
		return nil, fmt.Errorf("no shards found in stream %s", streamName)
	}
	shardId := *describeOutput.StreamDescription.Shards[0].ShardId

	c := &KinesisConsumer{
		client:     client,
		streamName: streamName,
		shardId:    shardId,
		msgChan:    make(chan testsuite.Message, 100),
		done:       make(chan struct{}),
	}
	c.wg.Add(1)
	go c.consume()
	return c, nil
}

func (c *KinesisConsumer) consume() {
	defer c.wg.Done()

	// Get shard iterator - using TRIM_HORIZON to get all records
	iteratorOutput, err := c.client.GetShardIterator(context.Background(), &kinesis.GetShardIteratorInput{
		StreamName:        aws.String(c.streamName),
		ShardId:           aws.String(c.shardId),
		ShardIteratorType: types.ShardIteratorTypeTrimHorizon,
	})
	if err != nil {
		fmt.Printf("Error getting shard iterator: %v\n", err)
		return
	}

	iterator := iteratorOutput.ShardIterator
	for {
		select {
		case <-c.done:
			return
		default:
			// Get records using the shard iterator
			recordsOutput, err := c.client.GetRecords(context.Background(), &kinesis.GetRecordsInput{
				ShardIterator: iterator,
				Limit:         aws.Int32(25),
			})
			if err != nil {
				fmt.Printf("Error getting records: %v\n", err)
				// Sleep briefly on error before trying again
				time.Sleep(1 * time.Second)
				continue
			}

			// Process each record
			for _, record := range recordsOutput.Records {
				var payload map[string]interface{}
				err := json.Unmarshal(record.Data, &payload)
				if err != nil {
					fmt.Printf("Error unmarshaling record data: %v\n", err)
					continue
				}

				// Extract metadata from the payload
				metadata := make(map[string]string)
				if metaMap, ok := payload["metadata"].(map[string]interface{}); ok {
					for k, v := range metaMap {
						if strVal, ok := v.(string); ok {
							metadata[k] = strVal
						}
					}
				}

				// Extract data
				var data []byte
				if dataMap, ok := payload["data"]; ok {
					data, _ = json.Marshal(dataMap)
				}

				if !c.shuttingDown {
					c.msgChan <- testsuite.Message{
						Data:     data,
						Metadata: metadata,
						Raw:      record,
					}
				}
			}

			// Update the iterator for the next call
			iterator = recordsOutput.NextShardIterator
			if iterator == nil {
				// End of shard, exit
				return
			}

			// If no records, sleep a bit to avoid hitting API limits
			if len(recordsOutput.Records) == 0 {
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
}

func (c *KinesisConsumer) Consume() <-chan testsuite.Message {
	return c.msgChan
}

func (c *KinesisConsumer) Close() error {
	c.shuttingDown = true
	close(c.done)
	c.wg.Wait()
	close(c.msgChan)
	return nil
}

// KinesisAsserter verifies Kinesis-specific aspects of the message
type KinesisAsserter struct{}

func (a *KinesisAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	// Metadata is already parsed in the consumer
	metadata := msg.Metadata

	// Verify system metadata
	assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present")
	assert.Equal(t, event.ID, metadata["event-id"], "event-id should match")
	assert.Equal(t, event.Topic, metadata["topic"], "topic should match")

	// Verify custom metadata
	for k, v := range event.Metadata {
		assert.Equal(t, v, metadata[k], "metadata key %s should match expected value", k)
	}

	// Verify Kinesis-specific properties if possible
	if record, ok := msg.Raw.(types.Record); ok {
		// Partition key should be the event ID in our basic implementation
		assert.Equal(t, event.ID, *record.PartitionKey, "partition key should be event ID")
	}
}

// Create or ensure Kinesis stream exists
func ensureKinesisStream(ctx context.Context, client *kinesis.Client, streamName string) error {
	// Check if stream exists
	_, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(streamName),
	})
	if err == nil {
		// Stream exists
		return nil
	}

	// Create the stream
	_, err = client.CreateStream(ctx, &kinesis.CreateStreamInput{
		StreamName: aws.String(streamName),
		ShardCount: aws.Int32(1),
	})
	if err != nil {
		return err
	}

	// Wait for stream to become active
	waiter := kinesis.NewStreamExistsWaiter(client)
	err = waiter.Wait(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(streamName),
	}, 30*time.Second)

	// Even though the stream is marked as ACTIVE, there can be a slight delay before it's fully
	// ready to accept writes/reads, especially in LocalStack. This sleep helps avoid flaky tests
	// that might fail if we try to use the stream immediately after creation.
	if err == nil {
		time.Sleep(2 * time.Second)
	}

	return err
}

// Delete Kinesis stream
func deleteKinesisStream(ctx context.Context, client *kinesis.Client, streamName string) error {
	_, err := client.DeleteStream(ctx, &kinesis.DeleteStreamInput{
		StreamName: aws.String(streamName),
	})
	return err
}

// AWSKinesisSuite is the test suite for AWS Kinesis
type AWSKinesisSuite struct {
	testsuite.PublisherSuite
	consumer   *KinesisConsumer
	client     *kinesis.Client
	streamName string
}

func TestAWSKinesisSuite(t *testing.T) {
	suite.Run(t, new(AWSKinesisSuite))
}

func (s *AWSKinesisSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))

	// Create a unique stream name for the test
	s.streamName = "test-stream-" + uuid.New().String()

	// Setup AWS config and client
	localstackEndpoint := testinfra.EnsureLocalStack()
	awsConfig, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(
				func(service, region string, options ...interface{}) (aws.Endpoint, error) {
					return aws.Endpoint{URL: localstackEndpoint}, nil
				})),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	// Create Kinesis client
	s.client = kinesis.NewFromConfig(awsConfig)

	// Create test stream
	err = ensureKinesisStream(context.Background(), s.client, s.streamName)
	require.NoError(t, err)

	// Create consumer
	s.consumer, err = NewKinesisConsumer(s.client, s.streamName)
	require.NoError(t, err)

	// Create provider
	provider, err := destawskinesis.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	// Create destination
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_kinesis"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"endpoint":    localstackEndpoint,
			"stream_name": s.streamName,
			"region":      "us-east-1",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test",
			"secret":  "test",
			"session": "",
		}),
	)

	// Initialize publisher suite with custom asserter
	testConfig := testsuite.Config{
		Provider: provider,
		Dest:     &destination,
		Consumer: s.consumer,
		Asserter: &KinesisAsserter{},
	}
	s.InitSuite(testConfig)
}

func (s *AWSKinesisSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
	if s.client != nil && s.streamName != "" {
		// Delete the test stream
		_ = deleteKinesisStream(context.Background(), s.client, s.streamName)
	}
}

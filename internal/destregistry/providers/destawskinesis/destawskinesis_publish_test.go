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
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/partitionkey"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/idgen"
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
	// lastSeq is the sequence number of the most recent record handed to the
	// suite. GetRecords can return a record that an earlier call already
	// returned — observed against localstack, with no error reported and the
	// iterator advanced normally. Every test in the suite reads one shared
	// channel, so a single redelivered record shifts every later read by one and
	// each test then verifies the wrong event. Records within a shard are
	// ordered, so dropping anything at or before lastSeq makes the stream
	// deliver-once from the suite's point of view.
	var lastSeq string
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
				if seq := aws.ToString(record.SequenceNumber); seq != "" {
					if lastSeq != "" && !sequenceAfter(seq, lastSeq) {
						continue
					}
					lastSeq = seq
				}
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

// sequenceAfter reports whether a is a later Kinesis sequence number than b.
// They are arbitrary-precision decimal integers, so compare by length first and
// lexically only at equal length.
func sequenceAfter(a, b string) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a > b
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
type KinesisAsserter struct {
	partitionKeyTemplate string // Stores the template string being used
}

// evaluateTemplate is a test helper that evaluates a JMESPath template against payload data
func (a *KinesisAsserter) evaluateTemplate(payload map[string]interface{}, eventID string) (string, error) {
	return partitionkey.Evaluate(a.partitionKeyTemplate, payload, eventID)
}

func (a *KinesisAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	// Metadata is already parsed in the consumer
	metadata := msg.Metadata

	// Verify system metadata
	assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present")
	testsuite.AssertTimestampIsISO8601(t, metadata["timestamp"])
	assert.Equal(t, event.ID, metadata["event-id"], "event-id should match")
	assert.Equal(t, event.Topic, metadata["topic"], "topic should match")

	// Verify custom metadata
	for k, v := range event.Metadata {
		assert.Equal(t, v, metadata[k], "metadata key %s should match expected value", k)
	}

	// Verify Kinesis-specific properties if possible
	if record, ok := msg.Raw.(types.Record); ok {
		if a.partitionKeyTemplate != "" {
			var payload map[string]interface{}
			err := json.Unmarshal(record.Data, &payload)
			if err != nil {
				t.Errorf("Error unmarshaling record data: %v", err)
				return
			}

			// Evaluate the template with our test helper
			expectedPartitionKey, err := a.evaluateTemplate(payload, event.ID)
			if err != nil {
				// If template evaluation fails, we expect fallback to event ID
				expectedPartitionKey = event.ID
				t.Errorf("Template evaluation failed: %v, expecting fallback to event ID", err)
			}

			assert.Equal(t, expectedPartitionKey, *record.PartitionKey,
				"partition key should match template evaluation result")
		} else {
			// Default behavior (no template) - partition key should be event ID
			assert.Equal(t, event.ID, *record.PartitionKey, "partition key should be event ID (default)")
		}
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

	// Wait for stream to become active with fast polling for tests
	waiter := kinesis.NewStreamExistsWaiter(client, func(o *kinesis.StreamExistsWaiterOptions) {
		o.MinDelay = 100 * time.Millisecond
		o.MaxDelay = 1 * time.Second
	})
	return waiter.Wait(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(streamName),
	}, 30*time.Second)
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
	consumer           *KinesisConsumer
	client             *kinesis.Client
	provider           destregistry.Provider
	localstackEndpoint string
	streamName         string
}

func TestAWSKinesisSuite(t *testing.T) {
	suite.Run(t, new(AWSKinesisSuite))
}

func (s *AWSKinesisSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))

	s.localstackEndpoint = testinfra.EnsureLocalStack()
	awsConfig, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	// Create Kinesis client with custom endpoint
	s.client = kinesis.NewFromConfig(awsConfig, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(s.localstackEndpoint)
	})

	s.provider, err = destawskinesis.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
}

// SetupTest gives each test its own stream and consumer. Sharing them across the
// suite means anything one test leaves behind — or that the stream redelivers —
// lands in the next test, which then verifies an event it never published.
func (s *AWSKinesisSuite) SetupTest() {
	t := s.T()

	s.streamName = "test-stream-" + idgen.String()
	require.NoError(t, ensureKinesisStream(context.Background(), s.client, s.streamName))

	consumer, err := NewKinesisConsumer(s.client, s.streamName)
	require.NoError(t, err)
	s.consumer = consumer

	partitionKeyTemplate := "join('__', [metadata.topic, metadata.timestamp, metadata.\"event-id\"])"
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_kinesis"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"endpoint":               s.localstackEndpoint,
			"stream_name":            s.streamName,
			"region":                 "us-east-1",
			"partition_key_template": partitionKeyTemplate,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test",
			"secret":  "test",
			"session": "",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: s.provider,
		Dest:     &destination,
		Consumer: s.consumer,
		Asserter: &KinesisAsserter{
			partitionKeyTemplate: partitionKeyTemplate,
		},
	})

	s.PublisherSuite.SetupTest()
}

func (s *AWSKinesisSuite) TearDownTest() {
	s.PublisherSuite.TearDownTest()

	if s.consumer != nil {
		s.consumer.Close()
		s.consumer = nil
	}
	if s.streamName != "" {
		_ = deleteKinesisStream(context.Background(), s.client, s.streamName)
		s.streamName = ""
	}
}

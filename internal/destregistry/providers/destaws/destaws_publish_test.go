package destaws_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destaws"
	"github.com/hookdeck/outpost/internal/util/awsutil"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

func TestIntegrationAWSDestination_Publish(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	// Get LocalStack config from testinfra
	awsConfig := testinfra.NewMQAWSConfig(t, nil)
	sqsClient, err := awsutil.SQSClientFromConfig(context.Background(), awsConfig.AWSSQS)
	require.NoError(t, err)
	queueURL, err := awsutil.EnsureQueue(context.Background(), sqsClient, awsConfig.AWSSQS.Topic, nil)
	require.NoError(t, err)

	// Create AWS provider
	provider, err := destaws.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	// Create test destination
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"endpoint":  awsConfig.AWSSQS.Endpoint, // LocalStack endpoint
			"queue_url": queueURL,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test",
			"secret":  "test",
			"session": "",
		}),
	)

	t.Run("should create publisher and publish message", func(t *testing.T) {
		ctx := context.Background()

		// Create publisher
		publisher, err := provider.CreatePublisher(ctx, &destination)
		require.NoError(t, err)
		defer publisher.Close()

		// Create test event
		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{
				"test_key": "test_value",
			}),
			testutil.EventFactory.WithMetadata(map[string]string{
				"meta_key": "meta_value",
			}),
		)

		// Publish event
		err = publisher.Publish(ctx, &event)
		require.NoError(t, err)

		// Receive message from SQS to verify
		result, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(queueURL),
			MaxNumberOfMessages:   1,
			WaitTimeSeconds:       5,
			MessageAttributeNames: []string{"All"},
		})
		require.NoError(t, err)
		require.Len(t, result.Messages, 1)

		msg := result.Messages[0]

		// Verify message body
		var body map[string]interface{}
		err = json.Unmarshal([]byte(*msg.Body), &body)
		require.NoError(t, err)
		require.Equal(t, "test_value", body["test_key"])

		// Verify metadata in message attributes
		require.Equal(t, "meta_value", *msg.MessageAttributes["meta_key"].StringValue)

		// Cleanup: Delete the message
		_, err = sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(queueURL),
			ReceiptHandle: msg.ReceiptHandle,
		})
		require.NoError(t, err)
	})
}

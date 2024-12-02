package destaws_test

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/destregistry"
	destaws "github.com/hookdeck/outpost/internal/destregistry/providers/destaws"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/awsutil"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
			"endpoint":  "https://sqs.us-east-1.amazonaws.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test-key",
			"secret":  "test-secret",
			"session": "test-session",
		}),
	)

	awsDestination, err := destaws.New()
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, awsDestination.Validate(nil, &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := awsDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing queue_url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"endpoint": "https://sqs.us-east-1.amazonaws.com",
		}
		err := awsDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.queue_url", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed queue_url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"queue_url": "not-a-valid-url",
			"endpoint":  "https://sqs.us-east-1.amazonaws.com",
		}
		err := awsDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.queue_url", validationErr.Errors[0].Field)
		assert.Equal(t, "format", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed endpoint", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
			"endpoint":  "not-a-valid-url",
		}
		err := awsDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.endpoint", validationErr.Errors[0].Field)
		assert.Equal(t, "format", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing credentials", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := awsDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		// Could be either key or secret that's reported first
		assert.Contains(t, []string{"credentials.key", "credentials.secret"}, validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})
}

func TestIntegrationAWSDestination_Publish(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	mq := testinfra.NewMQAWSConfig(t, nil)
	sqsClient, err := awsutil.SQSClientFromConfig(context.Background(), mq.AWSSQS)
	require.NoError(t, err)
	queueURL, err := awsutil.EnsureQueue(context.Background(), sqsClient, mq.AWSSQS.Topic, nil)
	require.NoError(t, err)
	awsdestination, err := destaws.New()
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"endpoint":  mq.AWSSQS.Endpoint,
			"queue_url": queueURL,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":    "key",
			"secret": "secret",
		}),
	)

	// Subscribe to messages
	errchan := make(chan error)
	msgchan := make(chan *types.Message)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		for {
			out, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:              &queueURL,
				MaxNumberOfMessages:   1,
				VisibilityTimeout:     3,
				WaitTimeSeconds:       10,
				MessageAttributeNames: []string{"All"},
			})
			log.Println("goroutine - received:", out, err)

			if err != nil {
				errchan <- err
				msgchan <- nil
				return
			}

			for _, m := range out.Messages {
				log.Println(m.MessageAttributes)

				// Delete message (to ack)
				_, err = sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      &queueURL,
					ReceiptHandle: m.ReceiptHandle,
				})
				if err != nil {
					errchan <- err
					msgchan <- nil
					return
				}
				errchan <- nil
				msgchan <- &m
				return
			}
		}
	}()

	// Act: Publish
	log.Println("publishing message")
	event := &models.Event{
		ID:               uuid.New().String(),
		TenantID:         uuid.New().String(),
		DestinationID:    destination.ID,
		Topic:            "test",
		EligibleForRetry: true,
		Time:             time.Now(),
		Metadata: map[string]string{
			"my_metadata":      "metadatavalue",
			"another_metadata": "anothermetadatavalue",
		},
		Data: map[string]interface{}{
			"mykey": "myvaluee",
		},
	}
	require.NoError(t, awsdestination.Publish(context.Background(), &destination, event))

	// Assert
	log.Println("waiting for msg...")
	err = <-errchan
	if err != nil {
		log.Println("error received:", err)
		require.Nil(t, err)
	}
	msg := <-msgchan
	require.NotNil(t, msg)
	log.Println("message received:", *msg)
	body := make(map[string]interface{})
	err = json.Unmarshal([]byte(*msg.Body), &body)
	require.Nil(t, err)
	assert.JSONEq(t, string(testutil.MustMarshalJSON(event.Data)), string(testutil.MustMarshalJSON(body)))
	// metadata
	if assert.NotNil(t, msg.MessageAttributes["my_metadata"].StringValue) {
		assert.Equal(t, "metadatavalue", *msg.MessageAttributes["my_metadata"].StringValue)
	}
	if assert.NotNil(t, msg.MessageAttributes["another_metadata"].StringValue) {
		assert.Equal(t, "anothermetadatavalue", *msg.MessageAttributes["another_metadata"].StringValue)
	}
}

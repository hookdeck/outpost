package aws_test

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	awsadapter "github.com/hookdeck/EventKit/internal/destinationadapter/adapters/aws"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := adapters.DestinationAdapterValue{
		ID:   uuid.New().String(),
		Type: "aws",
		Config: map[string]string{
			"queue_url": "url",
		},
		Credentials: map[string]string{
			"key":    "key",
			"secret": "secret",
			"token":  "token",
		},
	}

	awsdestination := awsadapter.New()

	t.Run("should not return error for valid destination", func(t *testing.T) {
		t.Parallel()

		err := awsdestination.Validate(nil, validDestination)

		assert.Nil(t, err)
	})

	t.Run("should validate type", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := awsdestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "invalid destination type")
	})

	t.Run("should validate config.queue_url", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{}
		err := awsdestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "queue_url is required for aws destination config")
	})

	t.Run("should validate credentials.key", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"notkey":  "key",
			"secret":  "secret",
			"session": "session",
		}
		err := awsdestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "key is required for aws destination credentials")
	})

	t.Run("should validate credentials.secret", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"key":       "key",
			"notsecret": "secret",
			"session":   "session",
		}
		err := awsdestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "secret is required for aws destination credentials")
	})

	t.Run("should allow empty credentials.session", func(t *testing.T) {
		t.Parallel()

		anotherDestination := validDestination
		anotherDestination.Credentials = map[string]string{
			"key":    "key",
			"secret": "secret",
		}
		err := awsdestination.Validate(nil, anotherDestination)

		assert.Nil(t, err)
	})
}

func TestIntegrationAWSDestination_Publish(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	// Setup SQS
	awsEndpoint, terminate, err := testutil.StartTestcontainerLocalstack()
	require.Nil(t, err)
	defer terminate()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queueName := "destination_sqs_queue"
	awsRegion := "eu-central-1"

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(awsRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.Nil(t, err)
	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(awsEndpoint)
	})
	queueURL, err := ensureQueue(ctx, sqsClient, queueName)
	require.Nil(t, err)

	// Setup Destination & Event
	awsdestination := awsadapter.New()

	destination := adapters.DestinationAdapterValue{
		ID:   uuid.New().String(),
		Type: "aws",
		Config: map[string]string{
			"endpoint":  awsEndpoint,
			"queue_url": queueURL,
		},
		Credentials: map[string]string{
			"key":    "key",
			"secret": "secret",
		},
	}

	// Subscribe to messages
	errchan := make(chan error)
	msgchan := make(chan *string)

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
				msgchan <- m.Body
				return
			}
		}
	}()

	// Act: Publish
	log.Println("publishing message")
	event := &ingest.Event{
		ID:               uuid.New().String(),
		TenantID:         uuid.New().String(),
		DestinationID:    destination.ID,
		Topic:            "test",
		EligibleForRetry: true,
		Time:             time.Now(),
		Metadata:         map[string]string{},
		Data: map[string]interface{}{
			"mykey": "myvaluee",
		},
	}
	err = awsdestination.Publish(context.Background(), destination, event)
	require.Nil(t, err)

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
	err = json.Unmarshal([]byte(*msg), &body)
	require.Nil(t, err)
	assert.Equal(t, event.Data, body)
}

func ensureQueue(ctx context.Context, sqsClient *sqs.Client, queueName string) (string, error) {
	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.(type) {
			case *types.QueueDoesNotExist:
				log.Println("Queue does not exist, creating...")
				createdQueue, err := sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{
					QueueName: aws.String(queueName),
				})
				if err != nil {
					return "", err
				}
				return *createdQueue.QueueUrl, nil
			default:
				return "", err
			}
		}
		return "", err
	}
	return *queue.QueueUrl, nil
}

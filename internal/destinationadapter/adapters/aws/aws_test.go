package aws_test

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/destinationadapter/adapters"
	awsadapter "github.com/hookdeck/outpost/internal/destinationadapter/adapters/aws"
	"github.com/hookdeck/outpost/internal/util/awsutil"
	"github.com/hookdeck/outpost/internal/util/testinfra"
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
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	mq := testinfra.NewMQAWSConfig(t, nil)
	sqsClient, err := awsutil.SQSClientFromConfig(context.Background(), mq.AWSSQS)
	require.NoError(t, err)
	queueURL, err := awsutil.EnsureQueue(context.Background(), sqsClient, mq.AWSSQS.Topic, nil)
	require.NoError(t, err)
	awsdestination := awsadapter.New()

	destination := adapters.DestinationAdapterValue{
		ID:   uuid.New().String(),
		Type: "aws",
		Config: map[string]string{
			"endpoint":  mq.AWSSQS.Endpoint,
			"queue_url": queueURL,
		},
		Credentials: map[string]string{
			"key":    "key",
			"secret": "secret",
		},
	}

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
	event := &adapters.Event{
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
	require.NoError(t, awsdestination.Publish(context.Background(), destination, event))

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
	assert.Equal(t, event.Data, body)
	// metadata
	if assert.NotNil(t, msg.MessageAttributes["my_metadata"].StringValue) {
		assert.Equal(t, "metadatavalue", *msg.MessageAttributes["my_metadata"].StringValue)
	}
	if assert.NotNil(t, msg.MessageAttributes["another_metadata"].StringValue) {
		assert.Equal(t, "anothermetadatavalue", *msg.MessageAttributes["another_metadata"].StringValue)
	}
}

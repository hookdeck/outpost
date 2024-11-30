package destrabbitmq_test

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": "localhost:5672",
			"exchange":   "test",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "guest",
			"password": "guest",
		}),
	)

	rabbitmqDestination := destrabbitmq.New()

	t.Run("should not return error for valid destination", func(t *testing.T) {
		t.Parallel()

		err := rabbitmqDestination.Validate(nil, &validDestination)

		assert.Nil(t, err)
	})

	t.Run("should validate type", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := rabbitmqDestination.Validate(nil, &invalidDestination)

		assert.ErrorContains(t, err, "invalid destination type")
	})

	t.Run("should validate config.server_url", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{}
		err := rabbitmqDestination.Validate(nil, &invalidDestination)

		assert.ErrorContains(t, err, "server_url is required for rabbitmq destination config")
	})

	t.Run("should validate config.exchange", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{"server_url": "localhost:5672"}
		err := rabbitmqDestination.Validate(nil, &invalidDestination)

		assert.ErrorContains(t, err, "exchange is required for rabbitmq destination config")
	})

	t.Run("should validate credentials.username", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{"password": "password"}
		err := rabbitmqDestination.Validate(nil, &invalidDestination)

		assert.ErrorContains(t, err, "username is required for rabbitmq destination credentials")
	})

	t.Run("should validate credentials.password", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{"username": "username"}
		err := rabbitmqDestination.Validate(nil, &invalidDestination)

		assert.ErrorContains(t, err, "password is required for rabbitmq destination credentials")
	})
}

func TestRabbitMQDestination_Publish(t *testing.T) {
	t.Parallel()

	rabbitmqDestination := destrabbitmq.New()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": "localhost:5672",
			"exchange":   "test",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "guest",
			"password": "guest",
		}),
	)

	t.Run("should validate before publish", func(t *testing.T) {
		t.Parallel()

		invalidDestination := destination
		invalidDestination.Type = "invalid"

		err := rabbitmqDestination.Publish(nil, &invalidDestination, nil)
		assert.ErrorContains(t, err, "invalid destination type")
	})
}

func TestIntegrationRabbitMQDestination_Publish(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	mq := testinfra.NewMQRabbitMQConfig(t)
	rabbitmqDestination := destrabbitmq.New()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": testutil.ExtractRabbitURL(mq.RabbitMQ.ServerURL),
			"exchange":   mq.RabbitMQ.Exchange,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": testutil.ExtractRabbitUsername(mq.RabbitMQ.ServerURL),
			"password": testutil.ExtractRabbitPassword(mq.RabbitMQ.ServerURL),
		}),
	)

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
			"mykey": "myvalue",
		},
	}

	readyChan := make(chan bool)
	cancelChan := make(chan bool)
	msgChan := make(chan *amqp091.Delivery)
	go func() {
		conn, _ := amqp091.Dial(mq.RabbitMQ.ServerURL)
		defer conn.Close()
		ch, _ := conn.Channel()
		defer ch.Close()

		msgs, _ := ch.Consume(
			mq.RabbitMQ.Queue, // queue
			"",                // consumer
			true,              // auto-ack
			false,             // exclusive
			false,             // no-local
			false,             // no-wait
			nil,               // args
		)

		log.Println("ready to receive messages")
		readyChan <- true

		go func() {
			for d := range msgs {
				msgChan <- &d
			}
		}()

		<-cancelChan
		msgChan <- nil
	}()

	<-readyChan
	log.Println("publishing message")
	assert.NoError(t, rabbitmqDestination.Publish(context.Background(), &destination, event))

	func() {
		time.Sleep(time.Second / 2)
		log.Println("cancelling")
		cancelChan <- true
	}()

	msg := <-msgChan
	if msg == nil {
		t.Fatal("no message received")
	}
	log.Println("message received", msg)
	body := make(map[string]interface{})
	require.NoError(t, json.Unmarshal(msg.Body, &body))
	assert.JSONEq(t, string(testutil.MustMarshalJSON(event.Data)), string(testutil.MustMarshalJSON(body)))
	// metadata
	assert.Equal(t, "metadatavalue", msg.Headers["my_metadata"])
	assert.Equal(t, "anothermetadatavalue", msg.Headers["another_metadata"])
}

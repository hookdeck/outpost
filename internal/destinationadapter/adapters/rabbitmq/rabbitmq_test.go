package rabbitmq_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters/rabbitmq"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
)

func TestRabbitMQDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := adapters.DestinationAdapterValue{
		ID:   uuid.New().String(),
		Type: "rabbitmq",
		Config: map[string]string{
			"server_url": "amqp://guest:guest@localhost:5672",
			"exchange":   "test",
		},
		Credentials: map[string]string{},
	}

	rabbitmqDestination := rabbitmq.New()

	t.Run("should not return error for valid destination", func(t *testing.T) {
		t.Parallel()

		err := rabbitmqDestination.Validate(nil, validDestination)

		assert.Nil(t, err)
	})

	t.Run("should validate type", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := rabbitmqDestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "invalid destination type")
	})

	t.Run("should validate config.server_url", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{}
		err := rabbitmqDestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "server_url is required for rabbitmq destination config")
	})

	t.Run("should validate config.exchange", func(t *testing.T) {
		t.Parallel()

		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{"server_url": "amqp://guest:guest@localhost:5672"}
		err := rabbitmqDestination.Validate(nil, invalidDestination)

		assert.ErrorContains(t, err, "exchange is required for rabbitmq destination config")
	})
}

func TestRabbitMQDestination_Publish(t *testing.T) {
	t.Parallel()

	rabbitmqDestination := rabbitmq.New()

	destination := adapters.DestinationAdapterValue{
		ID:   uuid.New().String(),
		Type: "rabbitmq",
		Config: map[string]string{
			"server_url": "amqp://guest:guest@localhost:5672",
			"exchange":   "test",
		},
		Credentials: map[string]string{},
	}

	t.Run("should validate before publish", func(t *testing.T) {
		t.Parallel()

		invalidDestination := destination
		invalidDestination.Type = "invalid"

		err := rabbitmqDestination.Publish(nil, invalidDestination, nil)
		assert.ErrorContains(t, err, "invalid destination type")
	})

	t.Run("should publish message to RabbitMQ", func(t *testing.T) {
		t.Parallel()

		const (
			RABBIT_SERVER_URL = "amqp://guest:guest@localhost:5672"
			RABBIT_EXCHANGE   = "destination_exchange"
			RABBIT_QUEUE      = "destination_queue_test"
		)

		destination := adapters.DestinationAdapterValue{
			ID:   uuid.New().String(),
			Type: "rabbitmq",
			Config: map[string]string{
				"server_url": RABBIT_SERVER_URL,
				"exchange":   RABBIT_EXCHANGE,
			},
			Credentials: map[string]string{},
		}

		event := &ingest.Event{
			ID:               uuid.New().String(),
			TenantID:         uuid.New().String(),
			DestinationID:    destination.ID,
			Topic:            "test",
			EligibleForRetry: true,
			Time:             time.Now(),
			Metadata:         map[string]string{},
			Data: map[string]interface{}{
				"mykey": "myvalue",
			},
		}

		readyChan := make(chan bool)
		cancelChan := make(chan bool)
		msgChan := make(chan *amqp091.Delivery)
		go func() {
			conn, _ := amqp091.Dial(RABBIT_SERVER_URL)
			defer conn.Close()
			ch, _ := conn.Channel()
			defer ch.Close()

			ch.ExchangeDeclare(
				RABBIT_EXCHANGE, // name
				"topic",         // type
				true,            // durable
				false,           // auto-deleted
				false,           // internal
				false,           // no-wait
				nil,             // arguments
			)
			q, _ := ch.QueueDeclare(
				RABBIT_QUEUE, // name
				false,        // durable
				false,        // delete when unused
				true,         // exclusive
				false,        // no-wait
				nil,          // arguments
			)
			ch.QueueBind(
				q.Name,          // queue name
				"",              // routing key
				RABBIT_EXCHANGE, // exchange
				false,
				nil,
			)

			msgs, _ := ch.Consume(
				RABBIT_QUEUE, // queue
				"",           // consumer
				true,         // auto-ack
				false,        // exclusive
				false,        // no-local
				false,        // no-wait
				nil,          // args
			)

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
		err := rabbitmqDestination.Publish(nil, destination, event)
		assert.Nil(t, err)

		func() {
			time.Sleep(time.Second / 2)
			cancelChan <- true
		}()

		msg := <-msgChan
		if msg == nil {
			t.Fatal("no message received")
		}
		// assert.Nil(t, 1)
		body := make(map[string]interface{})
		err = json.Unmarshal(msg.Body, &body)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, event.Data, body)
	})
}

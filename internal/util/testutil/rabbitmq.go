package testutil

import (
	"context"

	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/rabbitmq/amqp091-go"
)

func DeclareTestRabbitMQInfrastructure(ctx context.Context, config *mqs.RabbitMQConfig) error {
	conn, err := amqp091.Dial(config.ServerURL)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	err = ch.ExchangeDeclare(
		config.Exchange, // name
		"topic",         // type
		true,            // durable
		false,           // auto-deleted
		false,           // internal
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		return err
	}
	queue, err := ch.QueueDeclare(
		config.Queue, // name
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return err
	}
	return ch.QueueBind(
		queue.Name,      // queue name
		"",              // routing key
		config.Exchange, // exchange
		false,
		nil,
	)
}

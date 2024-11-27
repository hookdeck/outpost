package mqinfra

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/rabbitmq/amqp091-go"
)

func DeclareRabbitMQ(ctx context.Context, cfg *mqs.RabbitMQConfig) error {
	conn, err := amqp091.Dial(cfg.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	dlx := cfg.Exchange + ".dlx"
	dlq := cfg.Queue + ".dlq"

	// Declare target exchange & queue
	if err := ch.ExchangeDeclare(
		cfg.Exchange, // name
		"topic",      // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(
		cfg.Queue, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		amqp091.Table{
			"x-queue-type":           "quorum",
			"x-dead-letter-exchange": dlx,
			"x-delivery-limit":       5,
		}, // arguments
	); err != nil {
		return err
	}
	if err := ch.QueueBind(
		cfg.Queue,    // queue name
		"",           // routing key
		cfg.Exchange, // exchange
		false,
		nil,
	); err != nil {
		return err
	}

	// Declare dead-letter exchange & queue
	if err := ch.ExchangeDeclare(
		dlx,     // name
		"topic", // type
		true,    // durable
		false,   // auto-deleted
		false,   // internal
		false,   // no-wait
		nil,     // arguments
	); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(
		dlq,   // name
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		amqp091.Table{
			"x-queue-type": "quorum",
		}, // arguments
	); err != nil {
		return err
	}
	if err := ch.QueueBind(
		dlq, // queue name
		"",  // routing key
		dlx, // exchange
		false,
		nil,
	); err != nil {
		return err
	}

	return nil
}

func TeardownRabbitMQ(ctx context.Context, cfg *mqs.RabbitMQConfig) error {
	conn, err := amqp091.Dial(cfg.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	dlx := cfg.Exchange + ".dlx"
	dlq := cfg.Queue + ".dlq"

	if _, err := ch.QueueDelete(
		cfg.Queue, // name
		false,     // ifUnused
		false,     // ifEmpty
		false,     // noWait
	); err != nil {
		return err
	}
	if err := ch.ExchangeDelete(
		cfg.Exchange, // name
		false,        // ifUnused
		false,        // noWait
	); err != nil {
		return err
	}
	if _, err := ch.QueueDelete(
		dlq,   // name
		false, // ifUnused
		false, // ifEmpty
		false, // noWait
	); err != nil {
		return err
	}
	if err := ch.ExchangeDelete(
		dlx,   // name
		false, // ifUnused
		false, // noWait
	); err != nil {
		return err
	}
	return nil
}

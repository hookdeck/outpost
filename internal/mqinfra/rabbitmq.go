package mqinfra

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/rabbitmq/amqp091-go"
)

func DeclareRabbitMQ(ctx context.Context, cfg *mqs.QueueConfig, policy *mqs.Policy) error {
	if cfg.RabbitMQ == nil {
		return errors.New("failed assertion: cfg.RabbitMQ != nil") // IMPOSSIBLE
	}

	conn, err := amqp091.Dial(cfg.RabbitMQ.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	dlx := cfg.RabbitMQ.Exchange + ".dlx"
	dlq := cfg.RabbitMQ.Queue + ".dlq"

	// Declare target exchange & queue
	if err := ch.ExchangeDeclare(
		cfg.RabbitMQ.Exchange, // name
		"topic",               // type
		true,                  // durable
		false,                 // auto-deleted
		false,                 // internal
		false,                 // no-wait
		nil,                   // arguments
	); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(
		cfg.RabbitMQ.Queue, // name
		true,               // durable
		false,              // delete when unused
		false,              // exclusive
		false,              // no-wait
		amqp091.Table{
			"x-queue-type":           "quorum",
			"x-dead-letter-exchange": dlx,
			"x-delivery-limit":       policy.RetryLimit,
		}, // arguments
	); err != nil {
		return err
	}
	if err := ch.QueueBind(
		cfg.RabbitMQ.Queue,    // queue name
		"",                    // routing key
		cfg.RabbitMQ.Exchange, // exchange
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

func TeardownRabbitMQ(ctx context.Context, cfg *mqs.QueueConfig) error {
	if cfg.RabbitMQ == nil {
		return errors.New("failed assertion: cfg.RabbitMQ != nil") // IMPOSSIBLE
	}

	conn, err := amqp091.Dial(cfg.RabbitMQ.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	dlx := cfg.RabbitMQ.Exchange + ".dlx"
	dlq := cfg.RabbitMQ.Queue + ".dlq"

	if _, err := ch.QueueDelete(
		cfg.RabbitMQ.Queue, // name
		false,              // ifUnused
		false,              // ifEmpty
		false,              // noWait
	); err != nil {
		return err
	}
	if err := ch.ExchangeDelete(
		cfg.RabbitMQ.Exchange, // name
		false,                 // ifUnused
		false,                 // noWait
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

package mqinfra

import (
	"context"
	"errors"

	"github.com/rabbitmq/amqp091-go"
)

type infraRabbitMQ struct {
	cfg *MQInfraConfig
}

func (infra *infraRabbitMQ) Exist(ctx context.Context) (bool, error) {
	if infra.cfg == nil || infra.cfg.RabbitMQ == nil {
		return false, errors.New("failed assertion: cfg.RabbitMQ != nil") // IMPOSSIBLE
	}

	conn, err := amqp091.Dial(infra.cfg.RabbitMQ.ServerURL)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return false, err
	}
	defer ch.Close()

	dlq := infra.cfg.RabbitMQ.Queue + ".dlq"

	// Check if exchange exists using passive declare
	if err := ch.ExchangeDeclarePassive(
		infra.cfg.RabbitMQ.Exchange, // name
		"topic",                     // type
		true,                        // durable
		false,                       // auto-deleted
		false,                       // internal
		false,                       // no-wait
		nil,                         // arguments
	); err != nil {
		// If error is channel/connection closed, exchange doesn't exist
		if amqpErr, ok := err.(*amqp091.Error); ok && amqpErr.Code == 404 {
			return false, nil
		}
		return false, err
	}

	// Check if main queue exists using passive declare
	if _, err := ch.QueueDeclarePassive(
		infra.cfg.RabbitMQ.Queue, // name
		true,                     // durable
		false,                    // delete when unused
		false,                    // exclusive
		false,                    // no-wait
		nil,                      // arguments
	); err != nil {
		// If error is channel/connection closed, queue doesn't exist
		if amqpErr, ok := err.(*amqp091.Error); ok && amqpErr.Code == 404 {
			return false, nil
		}
		return false, err
	}

	// Check if DLQ exists using passive declare
	if _, err := ch.QueueDeclarePassive(
		dlq,   // name
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	); err != nil {
		// If error is channel/connection closed, queue doesn't exist
		if amqpErr, ok := err.(*amqp091.Error); ok && amqpErr.Code == 404 {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (infra *infraRabbitMQ) Declare(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.RabbitMQ == nil {
		return errors.New("failed assertion: cfg.RabbitMQ != nil") // IMPOSSIBLE
	}

	conn, err := amqp091.Dial(infra.cfg.RabbitMQ.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	dlq := infra.cfg.RabbitMQ.Queue + ".dlq"

	// Declare target exchange & queue
	if err := ch.ExchangeDeclare(
		infra.cfg.RabbitMQ.Exchange, // name
		"topic",                     // type
		true,                        // durable
		false,                       // auto-deleted
		false,                       // internal
		false,                       // no-wait
		nil,                         // arguments
	); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(
		infra.cfg.RabbitMQ.Queue, // name
		true,                     // durable
		false,                    // delete when unused
		false,                    // exclusive
		false,                    // no-wait
		amqp091.Table{
			"x-queue-type":              "quorum",
			"x-delivery-limit":          infra.cfg.Policy.RetryLimit,
			"x-dead-letter-exchange":    infra.cfg.RabbitMQ.Exchange,
			"x-dead-letter-routing-key": dlq,
		}, // arguments
	); err != nil {
		return err
	}
	if err := ch.QueueBind(
		infra.cfg.RabbitMQ.Queue,    // queue name
		infra.cfg.RabbitMQ.Queue,    // routing key
		infra.cfg.RabbitMQ.Exchange, // exchange
		false,
		nil,
	); err != nil {
		return err
	}

	// Declare dead-letter queue
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
		dlq,                         // queue name
		dlq,                         // routing key
		infra.cfg.RabbitMQ.Exchange, // exchange
		false,
		nil,
	); err != nil {
		return err
	}

	return nil
}

func (infra *infraRabbitMQ) TearDown(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.RabbitMQ == nil {
		return errors.New("failed assertion: cfg.RabbitMQ != nil") // IMPOSSIBLE
	}

	conn, err := amqp091.Dial(infra.cfg.RabbitMQ.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	dlq := infra.cfg.RabbitMQ.Queue + ".dlq"

	if _, err := ch.QueueDelete(
		infra.cfg.RabbitMQ.Queue, // name
		false,                    // ifUnused
		false,                    // ifEmpty
		false,                    // noWait
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
		infra.cfg.RabbitMQ.Exchange, // name
		false,                       // ifUnused
		false,                       // noWait
	); err != nil {
		return err
	}
	return nil
}

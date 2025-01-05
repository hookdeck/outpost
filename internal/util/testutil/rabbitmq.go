package testutil

import (
	"context"
	"strings"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/rabbitmq/amqp091-go"
)

func DeclareTestRabbitMQInfrastructure(ctx context.Context, config *mqs.RabbitMQConfig) error {
	conn, err := amqp091.Dial(config.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
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
		queue.Name,      // routing key
		config.Exchange, // exchange
		false,
		nil,
	)
}

func TeardownTestRabbitMQInfrastructure(ctx context.Context, cfg *mqs.RabbitMQConfig) error {
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
	if _, err := ch.QueueDelete(cfg.Queue, false, false, false); err != nil {
		return err
	}
	if err := ch.ExchangeDelete(cfg.Exchange, false, false); err != nil {
		return err
	}
	return nil
}

// ExtractRabbitURL extracts the address from the endpoint
// for example: amqp://guest:guest@localhost:5672 -> localhost:5672
func ExtractRabbitURL(endpoint string) string {
	return strings.Split(endpoint, "@")[1]
}

// ExtractRabbitUsername extracts the username from the endpoint
// for example: amqp://u:p@localhost:5672 -> u
func ExtractRabbitUsername(endpoint string) string {
	first := strings.Split(endpoint, "@")[0]
	creds := strings.Split(first, "://")[1]
	return strings.Split(creds, ":")[0]
}

// ExtractRabbitPassword extracts the password from the endpoint
// for example: amqp://u:p@localhost:5672 -> p
func ExtractRabbitPassword(endpoint string) string {
	first := strings.Split(endpoint, "@")[0]
	creds := strings.Split(first, "://")[1]
	return strings.Split(creds, ":")[1]
}

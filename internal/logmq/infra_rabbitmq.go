package logmq

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/rabbitmq/amqp091-go"
)

type LogRabbitMQInfra struct {
	config *mqs.RabbitMQConfig
}

var _ LogInfra = &LogRabbitMQInfra{}

func (i *LogRabbitMQInfra) DeclareInfrastructure(ctx context.Context) error {
	conn, err := amqp091.Dial(i.config.ServerURL)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	err = ch.ExchangeDeclare(
		i.config.Exchange, // name
		"topic",           // type
		true,              // durable
		false,             // auto-deleted
		false,             // internal
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		return err
	}
	queue, err := ch.QueueDeclare(
		i.config.Queue, // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		return err
	}
	return ch.QueueBind(
		queue.Name,        // queue name
		"",                // routing key
		i.config.Exchange, // exchange
		false,
		nil,
	)
}

func NewLogRabbitMQInfra(config *mqs.RabbitMQConfig) *LogRabbitMQInfra {
	return &LogRabbitMQInfra{
		config: config,
	}
}

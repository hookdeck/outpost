package deliverymq

import (
	"context"

	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/rabbitmq/amqp091-go"
)

type DeliveryRabbitMQInfra struct {
	config *mqs.RabbitMQConfig
}

var _ DeliveryInfra = &DeliveryRabbitMQInfra{}

func (i *DeliveryRabbitMQInfra) DeclareInfrastructure(ctx context.Context) error {
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

func NewDeliveryRabbitMQInfra(config *mqs.RabbitMQConfig) *DeliveryRabbitMQInfra {
	return &DeliveryRabbitMQInfra{
		config: config,
	}
}

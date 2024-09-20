package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/rabbitmq/amqp091-go"
)

const (
	RabbitMQServerURL    = "amqp://guest:guest@localhost:5672"
	RabbitMQPublishQueue = "publish"
)

func publishRabbitMQ(body map[string]interface{}) error {
	log.Printf("[x] Publishing RabbitMQ")

	conn, err := amqp091.Dial(RabbitMQServerURL)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}

	messageBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	ch.PublishWithContext(context.Background(),
		"",                   // exchange
		RabbitMQPublishQueue, // routing key
		false,                // mandatory
		false,                // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        []byte(messageBody),
		},
	)

	return nil
}

func declareRabbitMQ() error {
	log.Printf("[*] Declaring RabbitMQ Publish infra")
	conn, err := amqp091.Dial(RabbitMQServerURL)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	_, err = ch.QueueDeclare(
		RabbitMQPublishQueue, // name
		true,                 // durable
		false,                // delete when unused
		false,                // exclusive
		false,                // no-wait
		nil,                  // arguments
	)
	return err
}

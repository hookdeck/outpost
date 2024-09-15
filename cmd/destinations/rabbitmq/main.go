package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rabbitmq/amqp091-go"
)

const (
	RABBIT_SERVER_URL = "amqp://guest:guest@localhost:5672"
	RABBIT_EXCHANGE   = "destination_exchange"
	RABBIT_QUEUE      = "destination_queue"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	conn, err := amqp091.Dial(RABBIT_SERVER_URL)
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
		RABBIT_EXCHANGE, // name
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

	q, err := ch.QueueDeclare(
		RABBIT_QUEUE, // name
		false,        // durable
		false,        // delete when unused
		true,         // exclusive
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return err
	}
	err = ch.QueueBind(
		q.Name,          // queue name
		"",              // routing key
		RABBIT_EXCHANGE, // exchange
		false,
		nil,
	)

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		return err
	}

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for d := range msgs {
			log.Printf("[x] %s", d.Body)
		}
	}()

	log.Printf("[*] Waiting for logs. To exit press CTRL+C")
	<-termChan

	return nil
}

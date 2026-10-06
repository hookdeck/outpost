package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/cmd/destinations/internal/destenv"
	"github.com/rabbitmq/amqp091-go"
)

// Consumes a RabbitMQ destination: binds a queue to the exchange and prints
// every message. Defaults match the local destination stack (`make up/dest`).
var (
	RABBIT_SERVER_URL = destenv.Get("DEST_RABBITMQ_URL", "amqp://guest:guest@localhost:15672")
	RABBIT_EXCHANGE   = destenv.Get("DEST_RABBITMQ_EXCHANGE", "destination_exchange")
	RABBIT_QUEUE      = destenv.Get("DEST_RABBITMQ_QUEUE", "destination_queue")
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	conn, err := dialWithRetry(RABBIT_SERVER_URL, 30*time.Second)
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
		"#",             // routing key: Outpost publishes with the event topic, take all
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

// dialWithRetry retries while the broker starts: right after `make up/dest`
// RabbitMQ accepts TCP before it completes the AMQP handshake.
func dialWithRetry(url string, timeout time.Duration) (*amqp091.Connection, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := amqp091.Dial(url)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		log.Printf("[*] waiting for RabbitMQ at %s: %v", url, err)
		time.Sleep(time.Second)
	}
}

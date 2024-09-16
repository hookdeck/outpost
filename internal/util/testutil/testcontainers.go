package testutil

import (
	"context"
	"log"

	testrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

func StartTestcontainerRabbitMQ() (string, func(), error) {
	ctx := context.Background()

	rabbitmqContainer, err := testrabbitmq.Run(ctx,
		"rabbitmq:3-management-alpine",
	)

	if err != nil {
		log.Printf("failed to start container: %s", err)
		return "", func() {}, err
	}

	endpoint, err := rabbitmqContainer.PortEndpoint(ctx, "5672/tcp", "")
	if err != nil {
		log.Printf("failed to get endpoint: %s", err)
		return "", func() {}, err
	}
	log.Printf("RabbitMQ running at %s", endpoint)
	return "amqp://guest:guest@" + endpoint,
		func() {
			if err := rabbitmqContainer.Terminate(ctx); err != nil {
				log.Printf("failed to terminate container: %s", err)
			}
		},
		nil
}

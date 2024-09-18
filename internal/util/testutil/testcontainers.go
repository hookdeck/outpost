package testutil

import (
	"context"
	"log"
	"strings"

	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

func StartTestcontainerRabbitMQ() (string, func(), error) {
	ctx := context.Background()

	rabbitmqContainer, err := rabbitmq.Run(ctx,
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

func StartTestcontainerLocalstack() (string, func(), error) {
	ctx := context.Background()

	localstackContainer, err := localstack.Run(ctx,
		"localstack/localstack:latest",
	)

	if err != nil {
		log.Printf("failed to start container: %s", err)
		return "", func() {}, err
	}

	endpoint, err := localstackContainer.PortEndpoint(ctx, "4566/tcp", "")
	if err != nil {
		log.Printf("failed to get endpoint: %s", err)
		return "", func() {}, err
	}
	if !strings.Contains(endpoint, "http://") {
		endpoint = "http://" + endpoint
	}
	log.Printf("Localstack running at %s", endpoint)
	return endpoint,
		func() {
			if err := localstackContainer.Terminate(ctx); err != nil {
				log.Printf("failed to terminate container: %s", err)
			}
		},
		nil
}

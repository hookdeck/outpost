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

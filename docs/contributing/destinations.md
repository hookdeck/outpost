# Destinations

## Local Development

There are a few helper Go scripts to help with the development process of various destination types. In these example snippets, we use `localhost` as the example host. If your dev services run inside Docker, `localhost` will not work. You should use `host.docker.internal` instead.

### AWS

> We currently only support AWS SQS destination.

To test AWS SQS destination locally, you can use [localstack](https://github.com/localstack/localstack) which is a fully functional local AWS cloud stack. You can run the Docker image using the MQs Docker Compose file in this project.

```sh
$ cd build/dev/mqs
# .../hookdeck/outpost/build/dev/mqs
$ docker-compose up -d
```

You can run the local dev script to configure and subscribe to a SQS queue:

```sh
# back at root .../hookdeck/outpost directory
$ go run cmd/destinations/aws/main.go
.......... [*] Ready to receive messages.
	Endpoint: http://localhost:4566
	Queue: http://sqs.eu-central-1.localhost.localstack.cloud:4566/000000000000/destination_sqs_queue
.......... [*] Waiting for logs. To exit press CTRL+C
```

Using this credential, you can create an AWS destination and start receiving events:

```sh
$ curl --location 'localhost:4000/<TENANT_ID>/destinations' \
--header 'Content-Type: application/json' \
--header 'Authorization: ••••••' \
--data '{
    "type": "aws",
    "topics": ["*"],
    "config": {
        "endpoint": "http://localhost:4566",
        "queue_url": "http://sqs.eu-central-1.localhost.localstack.cloud:4566/000000000000/destination_sqs_queue"
    }
}'
```

```json
{
  "id": "...",
  "type": "aws",
  "topics": [
    "*"
  ],
  "config": {
    "endpoint": "http://localhost:4566",
    "queue_url": "http://sqs.eu-central-1.localhost.localstack.cloud:4566/000000000000/destination_sqs_queue"
  },
  "created_at": "...",
  "disabled_at": null
}

```

### RabbitMQ

To test RabbitMQ destination, make sure you have a running RabbitMQ instance. You can do so locally using the MQs Docker Compose file in this project.

```sh
$ cd build/dev/mqs
# .../hookdeck/outpost/build/dev/mqs
$ docker-compose up -d
```

You can visit the [RabbitMQ Management Interface](http://localhost:15672) to confirm that you have RabbitMQ running. (Small tip: the default credentials for the dashboard is `guest`:`guest`)

From then, you can run the local dev script to declare a simple exchange & with a queue subscripiton:

```sh
# back at root .../hookdeck/outpost directory
$ go run cmd/destinations/rabbitmq/main.go
```

The test exchange is `destination_exchange` and the test queue is `destination_queue`.

You can create a RabbitMQ destination to start receiving events:

```sh
$ curl --location 'localhost:4000/<TENANT_ID>/destinations' \
--header 'Content-Type: application/json' \
--header 'Authorization: ••••••' \
--data '{
    "type": "rabbitmq",
    "topics": ["*"],
    "config": {
        "server_url": "amqp://guest:guest@localhost:5672",
        "exchange": "destination_exchange"
    }
}'
```

```json
{
  "id": "...",
  "type": "rabbitmq",
  "topics": [
    "*"
  ],
  "config": {
    "server_url": "amqp://guest:guest@localhost:5672",
    "exchange": "destination_exchange"
  },
  "created_at": "...",
  "disabled_at": null
}
```

### Webhooks

To test local webhooks destination, you can run a local mock server:

```sh
$ go run cmd/destinations/webhooks/main.go
# [*] Server listening on port :4000

# or specify a preferred PORT
$ PORT=3000 go run cmd/destinations/webhooks/main.go
# [*] Server listening on port :3000
```

You can create a webhooks destination to start receiving events:

```sh
$ curl --location 'localhost:4000/<TENANT_ID>/destinations' \
--header 'Content-Type: application/json' \
--header 'Authorization: ••••••' \
--data '{
    "type": "webhook",
    "topics": ["*"],
    "config": {
        "url": "http://localhost:4444"
    }
}'
```

```json
{
  "id": "...",
  "type": "webhook",
  "topics": [
    "*"
  ],
  "config": {
    "url": "http://localhost:4444"
  },
  "created_at": "...",
  "disabled_at": null
}
```

# Destinations

## Local Development

`make up/dest` starts the brokers Outpost delivers to: RabbitMQ, LocalStack (AWS), the Pub/Sub emulator and Kafka (`make up/dest DEST="rabbitmq aws"` for some of them). The services of `make up` reach them by name (`dest-rabbitmq:5672`, `http://dest-aws:4566`, ...); from your machine they're on `localhost` (`15672`, `14566`, ...). See [Destination stack](test.md#destination-stack) for the full list.

The helpers in `cmd/destinations` create a queue, topic or bucket on that stack and print what arrives. Their defaults match the stack; environment variables at the top of each `main.go` change addresses and names (`DEST_`-prefixed, for example `DEST_SQS_QUEUE`, `DEST_RABBITMQ_QUEUE`), e.g. when two checkouts share one stack.

The examples below use the API of `make up` at `localhost:3333` with the API key `apikey`.

### AWS SQS

```sh
$ make up/dest DEST=aws
$ go run ./cmd/destinations/awssqs
.......... [*] Ready to receive messages.
	Endpoint: http://localhost:14566
	Queue: http://sqs.eu-central-1.localhost.localstack.cloud:4566/000000000000/destination_sqs_queue
```

Create a destination. Outpost runs in Docker, so `endpoint` is the in-network address; Outpost reads the region from the queue URL's host, so keep the URL the helper printed:

```sh
$ curl 'localhost:3333/api/v1/tenants/<TENANT_ID>/destinations' \
--header 'Content-Type: application/json' \
--header 'Authorization: Bearer apikey' \
--data '{
    "type": "aws_sqs",
    "topics": ["*"],
    "config": {
        "endpoint": "http://dest-aws:4566",
        "queue_url": "http://sqs.eu-central-1.localhost.localstack.cloud:4566/000000000000/destination_sqs_queue"
    },
    "credentials": {"key": "test", "secret": "test"}
}'
```

`cmd/destinations` also has `awskinesis`, `awss3` and `awseventbridge` on the same LocalStack.

### RabbitMQ

```sh
$ make up/dest DEST=rabbitmq
$ go run ./cmd/destinations/rabbitmq
```

The helper declares the exchange `destination_exchange` with a queue bound to it. The management UI is at [localhost:15673](http://localhost:15673) (`guest`/`guest`).

```sh
$ curl 'localhost:3333/api/v1/tenants/<TENANT_ID>/destinations' \
--header 'Content-Type: application/json' \
--header 'Authorization: Bearer apikey' \
--data '{
    "type": "rabbitmq",
    "topics": ["*"],
    "config": {
        "server_url": "dest-rabbitmq:5672",
        "exchange": "destination_exchange",
        "tls": "false"
    },
    "credentials": {"username": "guest", "password": "guest"}
}'
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

## Implementation

### Destination Registry

The destination registry serves as the core interface for managing and interacting with destination providers. It allows other services to validate configurations and publish messages. Each provider implements destination-specific logic for validation and publishing.

### Metadata

Provider metadata is organized in a standardized directory structure, with each provider having the following files:

- **`core.json`**: Defines configuration fields and basic validation rules.
- **`ui.json`**: Provides UI-specific information like labels, descriptions, and icons.
- **`instructions.md`**: Contains setup instructions for the provider.
- **`validation.json`**: Includes detailed JSON schema validation rules.

Custom metadata overrides can be applied through environment configuration.

### Destination Implementation Checklist

- [ ] Add provider metadata to `destregistry/metadata/providers/<provider>/` (`$ go run cmd/genprovider <provider>` for the provider metadata boilerplate)
- [ ] Implement provider interface in `destregistry/providers/<provider>`
- [ ] Run provider test suite (common test suite not implemented - TODO)
- [ ] Add provider to the default provider registration in `destregistry/providers/default.go`

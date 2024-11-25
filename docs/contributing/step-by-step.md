# Getting Started - Step by Step

Here's a more in-depth guide on how to set up your full environment with Docker. Here are things to look out for:

## Phase 1: Start environment

1: Copy `.env.example` to `.env` to use the default environment variables.

```sh
$ cp .env.example .env
```

2: Create `outpost` Docker network shared by all of your containers

```sh
$ make network
# docker network create outpost
```

3: Choose internal MQs

Outpost relies on a few mqs (deliverymq, logmq, etc.) to function. There's a [Docker Compose file](../../build/dev/mqs/compose.yml) that will start a few different supported queues:

- RabbitMQ
- AWS via LocalStack
- (coming) GCP PubSub
- (coming) Azure ServiceBus

You must choose one to run with your services and set it up in your `.env`.

- To configure AWS:

```
DELIVERY_AWS_SQS_ENDPOINT="http://aws:4566"
DELIVERY_AWS_SQS_REGION="eu-central-1"
DELIVERY_AWS_SQS_SERVICE_ACCOUNT_CREDS="test:test:"
DELIVERY_AWS_SQS_TOPIC="outpost_delivery"

LOG_AWS_SQS_ENDPOINT="http://aws:4566"
LOG_AWS_SQS_REGION="eu-central-1"
LOG_AWS_SQS_SERVICE_ACCOUNT_CREDS="test:test:"
LOG_AWS_SQS_TOPIC="outpost_log"
```

You can change the region of topic name as you see fit.

- To configure RabbitMQ

```
DELIVERY_RABBITMQ_SERVER_URL="amqp://guest:guest@rabbitmq:5672"
DELIVERY_RABBITMQ_EXCHANGE="outpost_delivery"
DELIVERY_RABBITMQ_QUEUE="outpost.delivery"

LOG_RABBITMQ_SERVER_URL="amqp://guest:guest@rabbitmq:5672"
LOG_RABBITMQ_EXCHANGE="outpost_log"
LOG_RABBITMQ_QUEUE="outpost.log"
```

4: Start services

```sh
# create a shared Docker network for all Outpost Docker Compose stacks
$ make network

$ make up

# ... to stop
$ make down
```

The command `make up` will start 2 Docker Compose stacks: [outpost](../../build/dev/compose.yml) and [mqs](../../build/dev/mqs/compose.yml).

Once started, please check the log to see when your service is up. As the Docker containers are tuned for development flow (with live reload), it will take a minute or so the first time as it proceeds to download dependencies. When the API server has started, you can run a healthcheck to confirm that the service is up:

```sh
$ curl localhost:3333/api/v1/healthz
OK%
```

## Phase 2: Create tenant & destination

1: Create a tenant with ID: 123

```sh
$ curl --location --request PUT 'localhost:3333/api/v1/123' \
--header 'Authorization: Bearer apikey'
{"id":"123","created_at":"..."}%
```

2: Create a webhook-type destination

```sh
$ curl --location 'localhost:3333/api/v1/123/destinations' \
--header 'Content-Type: application/json' \
--header 'Authorization: Bearer apikey' \
--data '{
    "type": "webhook",
    "topics": ["*"],
    "config": {
        "url": "http://host.docker.internal:4444"
    },
    "credentials": {
    }
}'
{"id":"abcxyz","type":"webhook","topics":["*"],"config":{"url":"http://host.docker.internal:4444"},"credentials":{},"created_at":"...","disabled_at":null}%
```

Feel free to confirm that the destination is successfully created either in Redis or by listing the destination for tenant 123:

```sh
$ curl --location 'localhost:3333/api/v1/123/destinations' \
--header 'Authorization: Bearer apikey'
```

3: Start a mock server that acts as the webhook destination

```sh
$ go run ./cmd/destinations/webhooks
[*] Server listening on port :4444
```

4: Publish an event

```sh
$ curl --location 'localhost:3333/api/v1/publish' \
--header 'Content-Type: application/json' \
--header 'Authorization: Bearer apikey' \
--data '{
    "tenant_id": "123",
    "topic": "user.created",
    "eligible_for_retry": true,
    "metadata": {
        "meta": "data"
    },
    "data": {
        "user_id": "userid"
    }
}'
```

Checking the mock webhook terminal, you should see that it has received a new request from your event above.

```sh
$ go run ./cmd/destinations/webhooks
[*] Server listening on port :4444
[x] POST / {"user_id":"userid"}
```

You can also confirm the data by checking the data in ClickHouse or querying the Event API:

```sh
$ curl --location 'localhost:3333/api/v1/123/events' \
--header 'Authorization: Bearer apikey'
```

## Others

### Portal

To get the portal URL:

```sh
$ curl --location 'localhost:3333/api/v1/123/portal' \
--header 'Authorization: Bearer apikey'
{"redirect_url":"http://localhost:3333?token=eyJH..."}%
```

You can read more about the portal setup in [this document](portal.md).

### PublishMQ

To test publishing via mq instead of the Publish API endpoint, you can setup publishmq. Similar to the deliverymq and logmq above, you can configure Outpost to subscribe to one of the supported queue. The main difference is that with deliverymq and logmq, Outpost will automatically configure the infrastructure with the env variable values. With publishmq, Outpost is not the owner of the infrastructure and instead only subscribe to receive messages from the mq. Because of that, you need to also run a script to configure the infrastructure first before using it.

There's a helper script that runs a server to help with the DX here.

```sh
$ go run ./cmd/publish
[*] Server listening on port :5555
```

This service exposes 2 endpoint: `POST /declare` and `POST /publish`. You can use `POST /declare` to configure the mq you'd like to use and `POST /publish` to add a new event to your publish queue. There are not many configuration you can use here in its current state, so if you'd like to customize further, you may need to edit the code. With that said, it should work out of the box if you follow along with this getting started guide.

The only configuration is both of these endpoint accept a query param `?method` where the value can be `aws` or `rabbitmq`. The publish endpoint can also accept `?method=http`. The publish endpoint will add the body JSON into the queue (or send a request to localhost:3333/api/v1/publish).

```sh
# step 1: declare
$ curl --location --request POST 'localhost:5555/declare?method=aws'
```

You should see a new log line in the publish helper server terminal.

```sh
$ go run ./cmd/publish
[*] Server listening on port :5555
[*] Declaring AWS Publish infra
```

Before sending a new event, you can configure the publishmq with environment variable, similar to the deliverymq and logmq previously:

```
# for aws sqs
# PUBLISH_AWS_SQS_ENDPOINT="http://aws:4566"
# PUBLISH_AWS_SQS_REGION="eu-central-1"
# PUBLISH_AWS_SQS_SERVICE_ACCOUNT_CREDS="test:test:"
# PUBLISH_AWS_SQS_TOPIC="publish_sqs_queue"

# for rabbitmq
# PUBLISH_RABBITMQ_SERVER_URL="amqp://guest:guest@rabbitmq:5672"
# PUBLISH_RABBITMQ_EXCHANGE=""
# PUBLISH_RABBITMQ_QUEUE="publish"
```

You can confirm that it's working by reading the log of the API service. On startup, it should log that it's `subscribing to PublishMQ`.

```sh
# step 2: publish
$ curl --location 'localhost:5555/publish?method=aws' \
--header 'Content-Type: application/json' \
--header 'Authorization: Bearer apikey' \
--data '{
    "tenant_id": "123",
    "topic": "user.created",
    "eligible_for_retry": true,
    "metadata": {
        "meta": "data"
    },
    "data": {
        "user_id": "userid"
    }
}'
```

You should see a new log line in the publish helper server terminal.

```sh
$ go run ./cmd/publish
[*] Server listening on port :5555
[*] Declaring AWS Publish infra
[x] Publishing AWS
```

You should also confirm that the event is delivered by checking the mock webhook server, the delivery service log, or ClickHouse data.

### OpenTelemetry

You need an OpenTelemetry backend to collect the telemetry data and visualize it. We currently support Uptrace during the development process for this:

```sh
$ make up/uptrace

# to stop
$ make down/uptrace
```

You can confirm that it's working by visiting http://localhost:14318.

To collect OTEL data, add these env variables:

```sh
OTEL_SERVICE_NAME=outpost
OTEL_EXPORTER_OTLP_ENDPOINT="uptrace:14317"
OTEL_EXPORTER_OTLP_HEADERS="uptrace-dsn=http://outpost_secret_token@uptrace:14318?grpc=14317"
```

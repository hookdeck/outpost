# Examples for Node.js / TypeScript

Create a `.env`:

```
# Base API URL of your Outpost installation
# Include the API and version path
# e.g. http://localhost:3333/api/v1
OUTPOST_API_BASE_URL=

# API Key for your Outpost installation
OUTPOST_API_KEY=

# Webhook endpoints to test the receipt of
# webhook events
ORG_1_ENDPOINT_1=
ORG_1_ENDPOINT_2=
ORG_2_ENDPOINT=
```

## Migration script

`src/migrate.ts`

Demonstrates how a migration script could work.

Run with:

```sh
npm run migrate
```

## Publish via API script

Uses the `REAL_TEST_ENDPOINT` value to identify what to publish.

`src/publish-api.ts`

Run with:

```sh
npm run publish-api
```

## Publish via RabbitMQ script

RabbitMQ is assumed to be accessible via `amqp://guest:guest@localhost:5673`. This can be overridden with the `RABBITMQ_URL` environment variable.

`src/publish-rabbitmq.ts`

Run with:

```sh
npm run publish-rabbitmq
```

## Publish via SQS script

Requires the following environment variables to be set:

```
SQS_QUEUE_URL=
AWS_REGION=
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_SECRET=
```

The user associated with the Access Key must have the following permissions:

- `sqs:SendMessage`
- `sqs:GetQueueAttributes`

`src/publish-sqs.ts`

Run with:

```sh
npm run publish-sqs
```

## Verification script

As part of ensuring events are delivered as expected you should also ensure that webhook signatures are in the expected formation.

This script provides two examples of webhook verification.

`src/verify-signature.ts`

Run with:

```sh
npm run verify
```

## Portal URLs

List the signed portal URLs

`src/portal-urls.ts`

Run with:

```sh
npm run portal-urls
```

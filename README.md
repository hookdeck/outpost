<br>

<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/outpost-logo-white.svg">
    <img alt="Outpost logo" src="images/outpost-logo-black.svg" width="40%">
  </picture>
</div>

<br>

<div align="center">
  
[![License](https://img.shields.io/badge/License-Apache--2.0-blue)](#license)
[![Go Report Card](https://goreportcard.com/badge/github.com/hookdeck/outpost)](https://goreportcard.com/report/github.com/hookdeck/outpost)
[![Issues - Outpost](https://img.shields.io/github/issues/hookdeck/outpost)](https://github.com/hookdeck/outpost/issues)
![GitHub Release](https://img.shields.io/github/v/release/hookdeck/outpost) [![Managed Service](https://img.shields.io/badge/Managed-Hookdeck%20Outpost-6B4FBB)](https://hookdeck.com/outpost)
  
</div>

<div align="center">
SDKs:

[![Go SDK](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/hookdeck/outpost/refs/heads/main/sdks/outpost-go/shields.json)](sdks/outpost-go/README.md)
[![TypeScript SDK](https://img.shields.io/npm/v/%40hookdeck%2Foutpost-sdk)](sdks/outpost-typescript/README.md)
[![Python SDK](https://img.shields.io/pypi/v/outpost-sdk)](sdks/outpost-python/README.md)

[Documentation](#documentation)
·
[Report a bug](issues/new?assignees=&labels=bug&projects=&template=bug_report.md&title=%F0%9F%90%9B+Bug+Report%3A+)
·
[Request a feature](issues/new?assignees=&labels=enhancement&projects=&template=feature_request.md&title=%F0%9F%9A%80+Feature%3A+)

</div>

<div align="center">
  <h1>Outbound Webhooks and Event Destinations Infrastructure</h1>
</div>

Production-ready infrastructure for sending webhooks and delivering events from your platform to your customers' systems. Self-host it anywhere, or use [Hookdeck Outpost](https://hookdeck.com/outpost) as a managed service.

Add outbound webhooks and [Event Destinations](https://eventdestinations.org) to your platform, with support for Webhooks, Hookdeck Event Gateway, Amazon EventBridge, AWS SQS, AWS S3, GCP Pub/Sub, RabbitMQ, and Kafka. Outpost handles retries, tenant isolation, observability, and provides a portal for your end users.

The runtime has minimal dependencies (Redis/Redis cluster, PostgreSQL, a supported message queue), is 100% backward compatible with your existing webhook implementation, and is optimized for high-throughput, low-cost operation.

Outpost is built and maintained by [Hookdeck](https://hookdeck.com). Written in Go. Distributed as a binary and Docker container. Licensed under Apache-2.0.

![Outpost architecture](docs/public/images/architecture.png)

Read [Outpost Concepts](https://hookdeck.com/docs/outpost/concepts) to learn more about the Outpost architecture and design.

## Features

- **Multi-tenant support**: Create multiple tenants on a single Outpost deployment.
- **User portal**: Allow customers to view delivery metrics, manage destinations, debug delivery issues, and observe their event destinations.
- **Delivery failure alerts**: Get notified when destinations are failing so you can act before your customers notice.
- **Event topics and topic-based subscriptions**: Supports the common publish and subscription paradigm to ease adoption and integration into existing systems.
- **At least once delivery guarantee**: Messages are guaranteed to be delivered at least once and never lost.
- **Automatic and manual retries**: Configure retry strategies for event destinations and manually trigger event delivery retries via the API or user portal.
- **Event fanout**: A message sent to a topic is replicated and delivered to multiple endpoints for parallel processing and asynchronous event notifications.
- **Publish events via the API or a queue**: Publish events using the Outpost API or configure Outpost to read events from a publish queue.
- **OpenTelemetry**: OTel standardized traces, metrics, and logs.
- **Webhook best practices**: Opt-out webhook best practices, such as headers for idempotency, timestamp and signature, and signature rotation.
- **SDKs and MCP server**: Go, Python, and TypeScript SDKs are available. Outpost also ships with an MCP server.
- **Event destination types**: Out of the box support for Webhooks, Hookdeck Event Gateway, Amazon EventBridge, AWS SQS, AWS S3, GCP Pub/Sub, RabbitMQ, and Kafka.

See the [Outpost Features](https://hookdeck.com/docs/outpost/features) for more information.

## Why Outpost

Outpost is a good fit if:

- You're adding outbound webhooks to your platform for the first time
- You're replacing a homegrown webhook system that's become a maintenance burden
- You want to offer your customers more than just HTTP callbacks (queues, brokers, buses)
- You need multi-tenant isolation with a customer-facing portal out of the box
- You want full control over your infrastructure and data

Outpost is backward compatible with your existing payload format, HTTP headers, and signatures — you can drop it into what you already have.

## Documentation

- [Overview](https://hookdeck.com/docs/outpost/overview)
- [Concepts](https://hookdeck.com/docs/outpost/concepts)
- [Quickstarts](https://hookdeck.com/docs/outpost/quickstarts)
- [Features](https://hookdeck.com/docs/outpost/features)
- [Guides](https://hookdeck.com/docs/outpost/guides)
- [API Reference](https://hookdeck.com/docs/outpost/api)
- [Configuration Reference](https://hookdeck.com/docs/outpost/self-hosting/configuration)

## Quickstart

### Deploy to Railway

[![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/deploy/outpost-starter?referralCode=NRulS_)

Once deployed, set the `TOPICS` environment variable to your supported topics (e.g. `TOPICS=user.created,user.updated,user.deleted`). You'll need the public Railway URL of your Outpost instance (`$OUTPOST_URL`) and the generated `API_KEY` to authenticate requests.

### Deploy locally with Docker

```bash
git clone https://github.com/hookdeck/outpost.git
cd outpost/examples/docker-compose/
cp .env.example .env
```

Update the `API_KEY` value in the `.env` file, then start the services:

```bash
docker-compose -f compose.yml -f compose-rabbitmq.yml -f compose-postgres.yml up
```

Outpost is now running on `localhost:3333`.

See the [Configuration Reference](https://outpost.hookdeck.com/docs/references/configuration) for Redis cluster setup, TLS, and other deployment options.

### Try it out

Set your environment variables:

```bash
export OUTPOST_URL=http://localhost:3333
export API_KEY=your_api_key
```

Create a tenant, add a webhook destination, and publish an event:

```bash
# Create a tenant
curl -X PUT "$OUTPOST_URL/api/v1/tenants/acme-corp" \
  -H "Authorization: Bearer $API_KEY"

# Create a webhook destination
curl "$OUTPOST_URL/api/v1/tenants/acme-corp/destinations" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "type": "webhook",
    "topics": ["*"],
    "config": { "url": "https://your-endpoint.com/webhooks" }
  }'

# Publish an event
curl "$OUTPOST_URL/api/v1/publish" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "tenant_id": "acme-corp",
    "topic": "user.created",
    "data": { "user_id": "123" }
  }'
```

Get a portal link for the tenant:

```bash
curl "$OUTPOST_URL/api/v1/tenants/acme-corp/portal" \
  -H "Authorization: Bearer $API_KEY"
```

Open the returned `redirect_url` to view the Outpost portal.

![Dashboard homepage](https://github.com/hookdeck/outpost/raw/main/docs/public/images/dashboard-homepage.png)

SDKs are available for [Go](https://github.com/hookdeck/outpost/blob/main/sdks/outpost-go/README.md), [TypeScript](https://github.com/hookdeck/outpost/blob/main/sdks/outpost-typescript/README.md), and [Python](https://github.com/hookdeck/outpost/blob/main/sdks/outpost-python/README.md). See the [full quickstart guides](https://outpost.hookdeck.com/docs/quickstarts) for step-by-step setup with your stack.

## Hookdeck Outpost (Managed)

Don't want to run the infrastructure yourself? [Hookdeck Outpost](https://hookdeck.com/outpost) is a fully managed version that runs the **exact same codebase** — no proprietary fork, no reduced feature set.

The managed service adds serverless scaling, SOC 2 compliance, SSO, RBAC, and usage-based pricing starting at $10 per million events.

[Get started with Hookdeck Outpost →](https://hookdeck.com/outpost)

## Contributing

See [CONTRIBUTING](CONTRIBUTING.md).

## License

This repository contains Outpost, covered under the [Apache License 2.0](LICENSE), except where noted (any Outpost logos or trademarks are not covered under the Apache License, and should be explicitly noted by a LICENSE file.)

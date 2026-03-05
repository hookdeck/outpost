# Outpost Examples

This directory contains various examples demonstrating how to use and deploy Outpost.

## Deployment Examples

*   **[docker-compose/](./docker-compose/)**: Configurations for running Outpost and its dependencies (like Redis, Postgres, RabbitMQ) locally using Docker Compose. Useful for development and testing environments.
*   **[kubernetes/](./kubernetes/)**: Example Helm values and setup scripts for deploying Outpost to a Kubernetes cluster.

## SDK Usage Examples

These examples demonstrate basic usage of the official Outpost SDKs for interacting with the Outpost Admin API.

*   **[sdk-go/](./sdk-go/)**: Example Go application using the `outpost-go` SDK.
*   **[sdk-python/](./sdk-python/)**: Example Python application using the `outpost-sdk` (Python).
*   **[sdk-typescript/](./sdk-typescript/)**: Example TypeScript application using the `@hookdeck/outpost-sdk`.

## Other Demos

*   **[demos/](./demos/)**: Contains various other demonstration applications or specific feature examples. (Explore this directory for more specific use cases).

Each subdirectory contains its own `README.md` with specific setup and execution instructions.

### Troubleshooting: auth example returns 401

If the auth example (admin key → get tenant token → list destinations with that token) fails with **401 Unauthorized** on the list call, the server is rejecting the tenant JWT. That operation is allowed when the JWT’s tenant matches the path; 401 means the server could not verify the JWT (e.g. `API_JWT_SECRET` not set or inconsistent on the deployment). See **[sdks/schemas/README.md](../sdks/schemas/README.md)** (section “Troubleshooting: auth example returns 401”) for the exact cause and how to fix it. To confirm the examples work end-to-end, run against a local Outpost with `API_JWT_SECRET` set.
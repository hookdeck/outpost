# Outpost Loadtesting Guide

This document describes the workflow for running loadtests against the Outpost service.

## Configuration

The loadtest configurations are stored in the `loadtest/config` directory:
- `env/`: Contains environment-specific configurations
- `scenarios/`: Contains test scenario configurations

### Environment Configuration

The `env/local.json` file contains settings specific to your testing environment:

```json
{
  "name": "local",
  "api": {
    "baseUrl": "http://localhost:3333",
    "timeout": "30s"
  },
  "mockWebhook": {
    "url": "http://localhost:48080",
    "destinationUrl": "http://host.docker.internal:48080",
    "verificationPollTimeout": "5s"
  },
  "redis": "redis://localhost:46379"
}
```

Key configuration sections:

- `api`: Describes the Outpost service API connection settings
  - When using a local deployment, the default `baseUrl` is `localhost:3333`
  - For Kubernetes deployments with multiple API nodes, you should configure ingress for load balancing and use a domain like `outpost-medium.acme.local` as the `baseUrl`

- `mockWebhook`: Contains two important URL configurations:
  - `url`: The URL that k6 will use to access the mock webhook service
  - `destinationUrl`: The URL that will be registered in Outpost when creating a destination. Because local Outpost is running in Kubernetes (minikube), we need to use `host.docker.internal` instead of `localhost` to allow the Kubernetes pods to reach the mock webhook service on your host machine

Make sure to adjust these values according to your specific infrastructure setup.

## Available Scripts

There are two main loadtest scripts:

1. `events-throughput`: This script provisions a single tenant with one destination (configured as a mock webhook) and sends events to it. It's used to test the throughput capacity of the system.

2. `events-verify`: This script queries the mock webhook destination to verify that events were delivered correctly. It ensures the reliability of the system.

Both scripts use Redis to store information about each loadtest run, which allows for correlation between test execution and verification.

### events-throughput Configuration

The `scenarios/events-throughput` directory contains JSON files that define how the throughput test will be executed. For example, a basic configuration looks like this:

```json
{
  "options": {
    "scenarios": {
      "events": {
        "rate": 1000,
        "timeUnit": "1s",
        "duration": "5m",
        "preAllocatedVUs": 1000,
        "maxVUs": 1500
      }
    }
  }
}
```

Key parameters to modify:
- `rate`: The desired number of events per second to send
- `duration`: How long k6 will send events (e.g., "5m" for 5 minutes)
- `preAllocatedVUs`: Number of virtual users to allocate (each VU can generate events)
- `maxVUs`: Maximum number of virtual users that can be created if needed

To simulate a high load of 1000 events per second, you would set `rate: 1000` and allocate enough VUs to handle this rate.

## Workflow

### Setup

First, ensure you have k6 installed. You can find installation instructions on the [official k6 installation page](https://k6.io/docs/get-started/installation/).

Then, run the Docker Compose stack to start the required services:
- Redis (for k6 loadtest flow)
- Mock webhook destination

```
docker-compose up -d
```

### Running Tests

1. Update the configuration files in `loadtest/config` to simulate your desired scenario and environment.

2. Set your environment variables and run the tests:

```
# Set required environment variables
export API_KEY=your_api_key_here
export TESTID=$(date +%s)

# Run the throughput test
API_KEY=$API_KEY TESTID=$TESTID ./run-test.sh events-throughput --environment local

# Run the verification test
API_KEY=$API_KEY TESTID=$TESTID MAX_ITERATIONS=1000 ./run-test.sh events-verify --environment local
```

The `TESTID` variable is used to correlate the tests, allowing the verification test to check the results of a specific throughput test run.

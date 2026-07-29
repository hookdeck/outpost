# Mock Webhook Service

A lightweight service that acts as a destination for webhook events during load testing. It receives webhook payloads, stores them in memory, and provides APIs to verify delivery status.

## Usage

### Running the Service

The easiest way to run the service is using the provided run script:

```bash
# Run directly with 'go run'
./scripts/run.sh

# Run on a specific port
./scripts/run.sh --port=9090

# Build and run the binary
./scripts/run.sh --mode=binary --build

# Run using Docker
./scripts/run.sh --mode=docker --docker-image=mock-webhook:latest

# Show all options
./scripts/run.sh --help
```

### Manual Building and Running

If you prefer, you can build and run the service manually:

```bash
# Build the service
cd loadtest/mock/webhook
go build -o mock-webhook

# Run the service
./mock-webhook
```

Or run directly with Go:

```bash
cd loadtest/mock/webhook
go run main.go
```

### Configuration

The service can be configured using environment variables:

- `PORT`: HTTP port to listen on (default: 8080)

### Docker

You can build and run the service using Docker:

#### Using the build script

The included build script provides a convenient way to build the Docker image with custom options:

```bash
# Build with default name (mock-webhook:latest)
./scripts/build.sh

# Build with custom name and tag
./scripts/build.sh --name=my-webhook-service --tag=v1.0.0

# Build and push to a container registry
./scripts/build.sh --name=registry.example.com/webhooks/mock-webhook --tag=v1.0.0 --push

# Show help
./scripts/build.sh --help
```

#### Manual Docker commands

Alternatively, use Docker commands directly:

```bash
# Build the image
docker build -t mock-webhook .

# Run the container
docker run -p 8080:8080 mock-webhook
```

## API Endpoints

### Receive Webhooks
- **Endpoint:** `POST /webhook`
- **Description:** Accepts webhook events and stores them for verification
- **Request:**
  - Any JSON payload
  - Event ID is extracted from the `x-outpost-event-id` header
  - Headers are captured
- **Response:**
  - Status: 200 OK
  - Body: `{"received": true, "id": "<extracted_id>"}`

### Verify Individual Event
- **Endpoint:** `GET /events/{eventId}`
- **Description:** Checks if a specific event was received
- **Response:**
  - Status: 200 OK if found
  - Body:
    ```json
    {
      "id": "event-123",
      "received_at": "2023-06-15T12:34:56Z",
      "payload": { ... },
      "headers": { ... }
    }
    ```
  - If not found: Status 404 Not Found

### Health Check
- **Endpoint:** `GET /health`
- **Description:** Service health check with basic stats
- **Response:**
  - Status: 200 OK
  - Body:
    ```json
    {
      "status": "healthy",
      "events_received": 1503,
      "events_stored": 1201,
      "uptime_seconds": 3600
    }
    ```

## Architecture

- Uses LRU cache with time-based expiration (default: 10 minutes)
- Thread-safe implementation for concurrent access
- Automatic eviction of oldest or expired entries
- Maximum cache size to prevent unbounded memory growth 
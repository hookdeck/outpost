# Outpost Data Seeder

A tool for seeding Outpost with fake tenant and destination data for testing and development.

## Features

- Creates multiple tenants with randomized IDs
- Creates 1-10 destinations per tenant (configurable)
- Supports multiple destination types: webhook, AWS SQS, RabbitMQ
- Generates realistic fake data using gofakeit
- Concurrent execution for fast seeding
- Progress tracking and error reporting

## Installation

```bash
go get github.com/brianvoe/gofakeit/v6
```

## Usage

```bash
# Show help
go run cmd/seed/main.go -help

# Run with defaults (100 tenants, 1-10 destinations each)
go run cmd/seed/main.go

# Skip confirmation prompt
go run cmd/seed/main.go -yes

# Custom configuration
go run cmd/seed/main.go \
  -server="http://localhost:3333/api/v1" \
  -apikey="your-api-key" \
  -tenants=500 \
  -min-destinations=2 \
  -max-destinations=20 \
  -concurrency=20 \
  -yes \
  -verbose
```

## Flags

- `-help`: Show help message and examples
- `-server`: Outpost server URL (default: "http://localhost:3333/api/v1")
- `-apikey`: API key for authentication (default: "apikey")
- `-tenants`: Number of tenants to create (default: 100)
- `-min-destinations`: Minimum destinations per tenant (default: 1)
- `-max-destinations`: Maximum destinations per tenant (default: 10)
- `-concurrency`: Number of concurrent workers (default: 10)
- `-yes`: Skip confirmation prompt (default: false)
- `-verbose`: Enable verbose output (default: false)

## Generated Data

### Tenant IDs
- Various formats: `org_<uuid>`, `team_<word>`, `user_<number>`, company names, etc.

### Destinations
- **Webhook**: Random URLs with secrets
- **AWS SQS**: Valid-looking queue URLs with AWS credentials
- **RabbitMQ**: AMQP connection strings with exchanges and routing keys

### Topics
- 30% chance of wildcard (`*`) for all topics
- Otherwise 1-5 random topics from a predefined list (user.*, order.*, payment.*, etc.)

## Example Output

### With Confirmation Prompt
```
=== Outpost Data Seeder Configuration ===
Server: http://localhost:3333/api/v1
Tenants to create: 100
Destinations per tenant: 1-10 (avg: 5)
Estimated total destinations: ~500
Concurrency: 10 workers

This will create approximately 100 tenants and 500 destinations.
Continue? (y/N): y

Starting seed process...

=== Seeding Complete ===
Tenants created: 100
Destinations created: 547
Errors encountered: 0
```

### With -yes Flag (No Prompt)
```
=== Outpost Data Seeder Configuration ===
Server: http://localhost:3333/api/v1
Tenants to create: 100
Destinations per tenant: 1-10 (avg: 5)
Estimated total destinations: ~500
Concurrency: 10 workers

Starting seed process...

=== Seeding Complete ===
Tenants created: 100
Destinations created: 547
Errors encountered: 0
```

## Performance

With default settings (10 concurrent workers), the tool can create:
- ~100 tenants with ~500 destinations in ~5-10 seconds
- ~1000 tenants with ~5000 destinations in ~30-60 seconds

Adjust the `-concurrency` flag based on your server's capacity.
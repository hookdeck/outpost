# Outpost Data Seeder

A utility that creates test data in Outpost by sending API requests to create tenants and destinations with mock data.

## Purpose

This tool is useful for:
- Testing tenant and destination-related logic
- Validating migrations (e.g., Redis key structure changes)
- Populating development environments with realistic data
- Load testing with configurable volumes of data

## Usage

```bash
# Show all available options
go run cmd/seed/main.go -help

# Run with defaults (100 tenants with random destinations)
go run cmd/seed/main.go

# Skip confirmation prompt
go run cmd/seed/main.go -yes

# Create custom volume of data
go run cmd/seed/main.go -tenants=500 -min-destinations=5 -max-destinations=20
```

## What It Creates

- **Tenants**: With various ID formats (UUIDs, prefixed IDs like `org_`, `team_`, `cus_`)
- **Destinations**: Webhook endpoints pointing to `mock.hookdeck.com`
- **Topics**: Limited set including `user.created`, `user.updated`, `user.deleted`

The tool uses concurrent workers to efficiently create large volumes of test data.
# Scripts Directory

This directory contains operational scripts for Redis cluster setup, migration, and validation.

## Migration Scripts

### `migrate-redis-keys.sh`
**Purpose**: Migrates legacy Redis keys to cluster-compatible hash-tagged format  
**Usage**: `./scripts/migrate-redis-keys.sh`  
**When**: REQUIRED before deploying new code with hash-tagged keys  
**Safety**: Idempotent, preserves original data, includes verification

## Validation Scripts

### `test-hash-slots.sh`
**Purpose**: Validates that tenant keys hash to the same Redis cluster slot  
**Usage**: `./scripts/test-hash-slots.sh`  
**When**: Troubleshooting cluster issues, verifying key patterns  
**Output**: Hash slot assignments for test tenant keys

### `test-transactions.sh`  
**Purpose**: Tests Redis transaction functionality in cluster mode  
**Usage**: `./scripts/test-transactions.sh`  
**When**: Validating cluster transaction support  
**Output**: Success/failure of same-tenant vs cross-tenant transactions

## Existing Scripts

### `test-setup-info.sh`
**Purpose**: Displays environment and setup information for testing  
**Usage**: `./scripts/test-setup-info.sh`  
**When**: Debugging test environment issues

## Prerequisites

All scripts require:
- Redis CLI installed (`redis-cli`)
- Environment variables in `.env` file
- Network access to Redis cluster

For Azure Managed Redis:
- TLS connection capability
- Proper authentication credentials

## Script Execution Order

For new deployments with existing data:
1. `./scripts/test-hash-slots.sh` (validate key patterns)
2. `./scripts/migrate-redis-keys.sh` (migrate data) 
3. `./scripts/test-transactions.sh` (validate transactions)
4. Deploy application code
5. `./examples/azure/diagnostics.sh` (end-to-end validation)
# Redis EXECABORT Reproduction Guide

Based on analysis of the codebase, the Redis EXECABORT error occurs in [`entity.go:292`](internal/models/entity.go:292) when `destination.Credentials.MarshalBinary()` fails during the Redis transaction.

## Root Cause

The failure path is:
1. POST request to `/api/v1/{tenant_id}/destinations` 
2. [`destination_handlers.go:99`](internal/services/api/destination_handlers.go:99) calls `h.entityStore.CreateDestination()`
3. [`entity.go:291`](internal/models/entity.go:291) starts Redis transaction with `TxPipelined()`  
4. [`entity.go:292`](internal/models/entity.go:292) calls `destination.Credentials.MarshalBinary()`
5. [`destination.go:176`](internal/models/destination.go:176) calls `json.Marshal(m)` on the credentials map
6. If `json.Marshal()` fails, the Redis transaction aborts with EXECABORT

## Most Effective Reproduction Methods

### Method 1: Invalid UTF-8 Characters (MOST LIKELY TO SUCCEED)

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer apiKey" \
  -d '{
    "type": "webhook",
    "topics": ["test.event"],  
    "config": {
      "url": "https://example.com/webhook"
    },
    "credentials": {
      "secret": "invalid_utf8_\xFF\xFE\xFD",
      "key": "contains\x00null\x01bytes"
    }
  }' \
  http://localhost:3333/api/v1/test-tenant/destinations
```

### Method 2: Non-Printable Control Characters

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer apiKey" \
  -d '{
    "type": "webhook", 
    "topics": ["test.event"],
    "config": {
      "url": "https://example.com/webhook"
    },
    "credentials": {
      "secret": "control_chars_\u0000\u0001\u0002\u001f",
      "data": "bell\u0007backspace\u0008"  
    }
  }' \
  http://localhost:3333/api/v1/test-tenant/destinations
```

### Method 3: Oversized Payload (Memory Exhaustion)

```bash
# Create a very large credential value
LARGE_VALUE=$(python3 -c "print('A' * 10485760)")  # 10MB string

curl -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer apiKey" \
  -d '{
    "type": "webhook",
    "topics": ["test.event"],
    "config": {
      "url": "https://example.com/webhook"
    },
    "credentials": {
      "secret": "whsec_abc123",
      "large_data": "'$LARGE_VALUE'"
    }
  }' \
  http://localhost:3333/api/v1/test-tenant/destinations
```

## Expected Results

When `json.Marshal()` fails in the Redis transaction, you should see:

1. **In Application Logs:**
   - Error from `json.Marshal()` (e.g., "invalid UTF-8 in string")
   - Redis transaction failure
   - 500 Internal Server Error returned to client

2. **In Redis Logs:**
   - `EXECABORT` message 
   - Transaction rollback

3. **Client Response:**
   - HTTP 500 status code
   - Internal server error response

## Why These Methods Work

- **Invalid UTF-8**: Go's `json.Marshal()` validates UTF-8 and fails on invalid sequences
- **Control Characters**: Some control chars cannot be marshaled to valid JSON
- **Oversized Payloads**: May cause memory allocation failures during marshaling

The key insight is that since `Credentials` is a `map[string]string`, the marshaling failure must come from the string values themselves being unmarshalable, not from complex object structures.
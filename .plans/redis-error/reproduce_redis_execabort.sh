#!/bin/bash

# Script to reproduce Redis EXECABORT error by causing marshaling failures
# in destination.Credentials.MarshalBinary() during Redis transaction

set -e

# Configuration
BASE_URL="http://localhost:3333/api/v1"
TENANT_ID="test-tenant"
API_KEY="apikey"  # Replace with actual API key

echo "=== Redis EXECABORT Reproduction Script ==="
echo "This script creates API requests that cause json.Marshal() failures"
echo "in destination.Credentials.MarshalBinary() during Redis transactions"
echo ""

# Function to make API request
make_request() {
    local payload="$1"
    local description="$2"
    
    echo "Testing: $description"
    echo "Payload: $payload"
    echo ""
    
    curl -X POST \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $API_KEY" \
        -d "$payload" \
        "$BASE_URL/$TENANT_ID/destinations" \
        -v 2>&1 | head -20
    
    echo ""
    echo "---"
    echo ""
}

# Test Case 1: Invalid UTF-8 sequences in credentials
# JSON marshaling fails when encountering invalid UTF-8 bytes
echo "=== Test Case 1: Invalid UTF-8 in credentials ==="
PAYLOAD_UTF8='{
  "type": "webhook",
  "topics": ["test.event"],
  "config": {
    "url": "https://example.com/webhook"
  },
  "credentials": {
    "secret": "invalid_utf8_\xff\xfe\xfd",
    "key": "test\x00null_byte"
  }
}'
make_request "$PAYLOAD_UTF8" "Invalid UTF-8 sequences in credentials"

# Test Case 2: Extremely large credential values that may cause memory issues
echo "=== Test Case 2: Oversized credential values ==="
LARGE_STRING=$(printf 'A%.0s' {1..1048576})  # 1MB string
PAYLOAD_LARGE='{
  "type": "webhook", 
  "topics": ["test.event"],
  "config": {
    "url": "https://example.com/webhook"
  },
  "credentials": {
    "secret": "'"$LARGE_STRING"'",
    "large_data": "'"$LARGE_STRING"'"
  }
}'
make_request "$PAYLOAD_LARGE" "Oversized credential values (1MB each)"

# Test Case 3: Credentials containing control characters
echo "=== Test Case 3: Control characters in credentials ==="
PAYLOAD_CONTROL='{
  "type": "webhook",
  "topics": ["test.event"], 
  "config": {
    "url": "https://example.com/webhook"
  },
  "credentials": {
    "secret": "control_chars_\u0000\u0001\u0002\u001f",
    "data": "tabs_and_newlines\t\n\r\f\b"
  }
}'
make_request "$PAYLOAD_CONTROL" "Control characters in credentials"

# Test Case 4: Credentials with nested JSON that becomes malformed when stringified
echo "=== Test Case 4: Malformed nested JSON structures ==="
PAYLOAD_NESTED='{
  "type": "webhook",
  "topics": ["test.event"],
  "config": {
    "url": "https://example.com/webhook"  
  },
  "credentials": {
    "secret": "whsec_abc123",
    "nested_json": "{\"incomplete\": \"json\", \"missing\":",
    "circular": "self_reference"
  }
}'
make_request "$PAYLOAD_NESTED" "Malformed nested JSON in credentials"

# Test Case 5: Very deep nested object structures (potential stack overflow)
echo "=== Test Case 5: Extremely deep nesting ==="
DEEP_JSON=""
for i in {1..1000}; do
    DEEP_JSON="{\"level$i\":$DEEP_JSON}"
done
DEEP_JSON="$DEEP_JSON$(printf '}%.0s' {1..1000})"

PAYLOAD_DEEP='{
  "type": "webhook",
  "topics": ["test.event"],
  "config": {
    "url": "https://example.com/webhook"
  },
  "credentials": {
    "secret": "whsec_abc123",
    "deep_structure": "'"$DEEP_JSON"'"
  }
}'
make_request "$PAYLOAD_DEEP" "Extremely deep nested structure"

echo "=== Test Complete ==="
echo ""
echo "The above requests are designed to trigger json.Marshal() failures during"
echo "the Redis transaction in entity.go:292, causing EXECABORT errors."
echo ""
echo "Expected behavior:"
echo "- Invalid UTF-8 should cause json.Marshal to fail with invalid UTF-8 error"
echo "- Oversized payloads may cause memory allocation failures"
echo "- Control characters may cause marshaling issues"
echo "- Malformed JSON strings may cause parsing issues during unmarshaling"
echo "- Deep nesting may cause stack overflow during marshaling"
echo ""
echo "Monitor Redis logs and application logs for EXECABORT errors and"
echo "marshaling failure messages."
#!/bin/bash

# Mock Webhook Throughput Test Script
# This script tests the mock webhook service directly to measure its capacity

set -e

# Default values
RATE="${RATE:-10}"
DURATION="${DURATION:-30s}"
PRE_ALLOCATED_VUS="${PRE_ALLOCATED_VUS:-20}"
MAX_VUS="${MAX_VUS:-1000}"
WEBHOOK_URL="${WEBHOOK_URL:-http://localhost:8080/webhook}"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Help function
show_help() {
  echo "Usage: $0 [options]"
  echo ""
  echo "Test the mock webhook service throughput directly"
  echo ""
  echo "Environment Variables:"
  echo "  RATE                Target requests per second (default: 10)"
  echo "  DURATION            Test duration (default: 30s)"
  echo "  PRE_ALLOCATED_VUS   Pre-allocated virtual users (default: 20)"
  echo "  MAX_VUS             Maximum virtual users (default: 1000)"
  echo "  WEBHOOK_URL         Mock webhook URL (default: http://localhost:8080/webhook)"
  echo ""
  echo "Options:"
  echo "  --help              Show this help message"
  echo ""
  echo "Examples:"
  echo "  # Test with defaults (10 req/s for 30s)"
  echo "  $0"
  echo ""
  echo "  # Test with 100 req/s for 1 minute"
  echo "  RATE=100 DURATION=1m $0"
  echo ""
  echo "  # Test against production mock webhook"
  echo "  WEBHOOK_URL=https://webhook-mock-production.up.railway.app/webhook RATE=50 $0"
  echo ""
}

# Parse arguments
for arg in "$@"; do
  case $arg in
    --help)
      show_help
      exit 0
      ;;
    *)
      echo "Unknown option: $arg"
      show_help
      exit 1
      ;;
  esac
done

echo -e "${GREEN}=== Mock Webhook Throughput Test ===${NC}"
echo -e "${YELLOW}Configuration:${NC}"
echo "  Webhook URL: $WEBHOOK_URL"
echo "  Rate: $RATE req/s"
echo "  Duration: $DURATION"
echo "  Pre-allocated VUs: $PRE_ALLOCATED_VUS"
echo "  Max VUs: $MAX_VUS"
echo ""

# Check if k6 is installed
if ! command -v k6 &> /dev/null; then
  echo "Error: k6 is not installed. Please install k6 first:"
  echo "  https://k6.io/docs/get-started/installation/"
  exit 1
fi

# Get the directory of this script
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

# Run the k6 test
echo -e "${GREEN}Starting test...${NC}"
echo ""

cd "$PROJECT_ROOT"

k6 run \
  -e RATE="$RATE" \
  -e DURATION="$DURATION" \
  -e PRE_ALLOCATED_VUS="$PRE_ALLOCATED_VUS" \
  -e MAX_VUS="$MAX_VUS" \
  -e WEBHOOK_URL="$WEBHOOK_URL" \
  src/tests/webhook-mock-throughput.ts

echo ""
echo -e "${GREEN}Test complete!${NC}"

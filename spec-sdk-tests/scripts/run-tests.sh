#!/bin/bash

# Script to run contract tests with the Speakeasy SDK
set -e

# Change to the script's directory to ensure correct paths
cd "$(dirname "$0")/.."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Load environment variables from .env file if it exists
if [ -f .env ]; then
    echo -e "${YELLOW}Loading environment variables from .env...${NC}"
    set -o allexport
    source .env
    set +o allexport
    echo -e "${GREEN}✓ Environment variables loaded${NC}"
    echo ""
else
    echo -e "${YELLOW}⚠ No .env file found${NC}"
    echo "Please create a .env file with required configuration."
    echo "See .env.example for reference."
    echo ""
fi

echo -e "${GREEN}Starting Outpost Contract Tests${NC}"
echo ""

# Check if API_KEY is set
echo -e "${YELLOW}Checking API_KEY configuration...${NC}"
if [ -z "${API_KEY}" ]; then
    echo -e "${RED}Error: API_KEY environment variable is not set${NC}"
    echo ""
    echo "Please set API_KEY in your .env file:"
    echo "  1. Copy .env.example to .env: cp .env.example .env"
    echo "  2. Set API_KEY in .env to match your Outpost server"
    echo "  3. Ensure your Outpost server has the same API_KEY configured"
    echo ""
    exit 1
fi
echo -e "${GREEN}✓ API_KEY is configured${NC}"
echo ""

# Check if API is running (skip when using managed Outpost where /healthz is not available)
echo -e "${YELLOW}Checking if Outpost API is running...${NC}"
SKIP_HEALTH_CHECK_VAL="${SKIP_HEALTH_CHECK:-false}"
if [ "$SKIP_HEALTH_CHECK_VAL" = "true" ] || [ "$SKIP_HEALTH_CHECK_VAL" = "1" ] || [ "$SKIP_HEALTH_CHECK_VAL" = "yes" ]; then
    echo -e "${YELLOW}Skipping health check (SKIP_HEALTH_CHECK=true / managed Outpost)${NC}"
else
    API_URL=${API_BASE_URL:-http://localhost:3333}
    # Strip /api/v1 or similar path if present for healthz
    API_URL="${API_URL%%/api/*}"
    if ! curl -s -f -o /dev/null "$API_URL/healthz" 2>/dev/null; then
        echo -e "${RED}Error: Outpost API is not running at $API_URL${NC}"
        echo "Please start Outpost before running tests."
        echo ""
        echo "Example:"
        echo "  cd /path/to/outpost"
        echo "  go run ./cmd/api"
        echo ""
        echo "For managed Outpost (api.outpost.hookdeck.com), set SKIP_HEALTH_CHECK=true in .env"
        exit 1
    fi
    echo -e "${GREEN}✓ Outpost API is running${NC}"
fi
echo ""

echo -e "${GREEN}Running contract tests...${NC}"
echo ""

# Run tests
npm test

TEST_EXIT_CODE=$?

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed!${NC}"
else
    echo -e "${RED}✗ Tests failed${NC}"
fi

exit $TEST_EXIT_CODE
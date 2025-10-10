#!/bin/bash

# Script to run contract tests with Prism proxy
set -e

# Cleanup function
cleanup() {
    if [ "$CLEANUP_PROXY" = true ] && [ -n "$PRISM_PID" ]; then
        echo ""
        echo -e "${YELLOW}Stopping Prism proxy...${NC}"
        # Kill the process group to ensure all child processes are killed
        pkill -P $PRISM_PID 2>/dev/null || true
        kill $PRISM_PID 2>/dev/null || true
        sleep 1
        # Force kill if still running
        kill -9 $PRISM_PID 2>/dev/null || true
        echo -e "${GREEN}✓ Prism proxy stopped${NC}"
    fi
}

# Trap to ensure cleanup runs on exit or interrupt
trap cleanup EXIT INT TERM

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Load environment variables from .env file if it exists
if [ -f .env ]; then
    echo -e "${YELLOW}Loading environment variables from .env...${NC}"
    export $(cat .env | grep -v '^#' | grep -v '^$' | xargs)
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

# Check if API is running
echo -e "${YELLOW}Checking if Outpost API is running...${NC}"
API_URL=${API_DIRECT_URL:-http://localhost:3333}

if ! curl -s -f -o /dev/null "$API_URL/healthz" 2>/dev/null; then
    echo -e "${RED}Error: Outpost API is not running at $API_URL${NC}"
    echo "Please start Outpost before running tests."
    echo ""
    echo "Example:"
    echo "  cd /path/to/outpost"
    echo "  go run cmd/outpost/main.go"
    exit 1
fi

echo -e "${GREEN}✓ Outpost API is running${NC}"
echo ""

# Check if Prism proxy is running
echo -e "${YELLOW}Checking if Prism proxy is running...${NC}"
PROXY_URL=${API_PROXY_URL:-http://localhost:9000}

# Check if port 9000 is in use (better than checking HTTP response)
if ! lsof -i :9000 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo -e "${YELLOW}⚠ Prism proxy is not running${NC}"
    echo "Starting Prism proxy in background..."
    
    # Start Prism proxy in background
    npm run prism:proxy > prism-proxy.log 2>&1 &
    PRISM_PID=$!
    
    # Wait for proxy to start
    echo "Waiting for Prism proxy to start..."
    sleep 5
    
    # Check if the process is still running and port is listening
    if ! kill -0 $PRISM_PID 2>/dev/null || ! lsof -i :9000 -sTCP:LISTEN -t >/dev/null 2>&1; then
        echo -e "${RED}Error: Failed to start Prism proxy${NC}"
        echo "Check prism-proxy.log for details"
        kill $PRISM_PID 2>/dev/null || true
        exit 1
    fi
    
    echo -e "${GREEN}✓ Prism proxy started (PID: $PRISM_PID)${NC}"
    CLEANUP_PROXY=true
else
    echo -e "${GREEN}✓ Prism proxy is already running${NC}"
    CLEANUP_PROXY=false
fi

echo ""
echo -e "${GREEN}Running contract tests...${NC}"
echo ""

# Disable exit on error for test execution
set +e

# Run tests
npm test

TEST_EXIT_CODE=$?

# Re-enable exit on error
set -e

# Note: cleanup will be handled by the trap

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed!${NC}"
else
    echo -e "${RED}✗ Tests failed${NC}"
fi

exit $TEST_EXIT_CODE
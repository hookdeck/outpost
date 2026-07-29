#!/bin/bash

# Default values
TEST_FILE="src/tests/health.ts"
SCENARIO="basic"
ENVIRONMENT="local"
EXTRA_ARGS=""
K6_ARGS=""

# Print usage information
function show_usage {
  echo "Usage: ./run-test.sh [options] test_name [-- k6_options]"
  echo ""
  echo "Options:"
  echo "  --scenario SCENARIO      Specify scenario name (default: basic)"
  echo "  --environment ENV        Specify environment (default: local)"
  echo "  -e KEY=VALUE             Pass environment variable to k6"
  echo "  -- [k6_options]          Pass all arguments after -- directly to k6"
  echo ""
  echo "Examples:"
  echo "  ./run-test.sh events-throughput"
  echo "  ./run-test.sh events-verify -e TESTID=123456789"
  echo "  ./run-test.sh events-throughput -- --out json=results.json"
  echo "  ./run-test.sh events-verify -e TESTID=123456789 -- --out csv=results.csv"
  echo ""
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --help|-h)
      show_usage
      exit 0
      ;;
    --scenario)
      SCENARIO="$2"
      shift 2
      ;;
    --environment)
      ENVIRONMENT="$2"
      shift 2
      ;;
    -e)
      # Handle environment variables passed to k6
      EXTRA_ARGS="$EXTRA_ARGS $1 $2"
      # If this is setting TESTID, extract it
      if [[ "$2" == TESTID=* ]]; then
        TESTID="${2#TESTID=}"
      fi
      shift 2
      ;;
    --)
      # All remaining arguments are passed directly to k6
      shift
      K6_ARGS="$@"
      break
      ;;
    *)
      if [[ -z "$TEST_FILE" || "$TEST_FILE" == "src/tests/health.ts" ]]; then
        TEST_FILE="src/tests/$1.ts"
        shift
      else
        echo "Unexpected argument: $1"
        show_usage
        exit 1
      fi
      ;;
  esac
done

# Generate test ID using timestamp only if not already provided
if [ -z "$TESTID" ]; then
  TESTID=$(date +%s)
  echo "Generated TESTID: $TESTID"
fi

# Run k6 test with environment variables
TESTID=$TESTID \
  SCENARIO=$SCENARIO \
  ENVIRONMENT=$ENVIRONMENT \
  k6 run $EXTRA_ARGS "$TEST_FILE" $K6_ARGS 
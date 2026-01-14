#!/usr/bin/env bash
set -e

# Test runner script for Outpost
# Supports gotestsum (default) with fallback to go test

# Configuration via environment variables
TEST="${TEST:-./internal/...}"
RUN="${RUN:-}"
TESTARGS="${TESTARGS:-}"

# Determine runner: gotestsum if available, otherwise go test
if [ -n "$RUNNER" ]; then
    # Explicit RUNNER set
    :
elif command -v gotestsum &> /dev/null; then
    RUNNER="gotestsum"
else
    RUNNER="go"
fi

# run_tests executes tests using the configured runner
# Arguments: $@ - additional flags to pass to the test command
run_tests() {
    local pkg="$1"
    shift
    local extra_flags=("$@")

    # Build run flag if RUN is set
    local run_flag=()
    if [ -n "$RUN" ]; then
        run_flag=("-run" "$RUN")
    fi

    if [ "$RUNNER" = "gotestsum" ]; then
        gotestsum --rerun-fails=2 --hide-summary=skipped --format-hide-empty-pkg \
            --packages="$pkg" -- $TESTARGS "${extra_flags[@]}" "${run_flag[@]}"
    else
        go test "$pkg" $TESTARGS "${extra_flags[@]}" "${run_flag[@]}"
    fi
}

# Command: test - run unit + integration tests
cmd_test() {
    run_tests "$TEST"
}

# Command: unit - run tests with -short flag
cmd_unit() {
    run_tests "$TEST" -short
}

# Command: e2e - run end-to-end tests
cmd_e2e() {
    run_tests "./cmd/e2e"
}

# Command: full - run all tests with full backend compatibility
cmd_full() {
    export TESTCOMPAT=1
    echo "Running full test suite with TESTCOMPAT=1..."
    run_tests "$TEST"
    echo ""
    echo "Running e2e tests..."
    run_tests "./cmd/e2e"
}

# Main entry point
case "${1:-test}" in
    test)
        cmd_test
        ;;
    unit)
        cmd_unit
        ;;
    e2e)
        cmd_e2e
        ;;
    full)
        cmd_full
        ;;
    *)
        echo "Usage: $0 {test|unit|e2e|full}"
        echo ""
        echo "Commands:"
        echo "  test    Run unit + integration tests (default)"
        echo "  unit    Run tests with -short flag (skip long-running tests)"
        echo "  e2e     Run end-to-end tests (./cmd/e2e)"
        echo "  full    Run full test suite with TESTCOMPAT=1 (test + e2e)"
        echo ""
        echo "Environment variables:"
        echo "  TEST          Package(s) to test (default: ./internal/...)"
        echo "  RUN           Filter tests by name pattern"
        echo "  TESTARGS      Additional arguments to pass to test command"
        echo "  TESTINFRA     Set to 1 to enable infrastructure-dependent tests"
        echo "  TESTCOMPAT    Set to 1 to run full backend compatibility suite"
        echo "  TESTREDISCLUSTER  Set to 1 to enable Redis cluster tests"
        echo "  RUNNER        Force test runner: 'gotestsum' or 'go' (default: auto-detect)"
        exit 1
        ;;
esac

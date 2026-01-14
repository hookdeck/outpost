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

# Main entry point
case "${1:-test}" in
    test)
        cmd_test
        ;;
    *)
        echo "Usage: $0 {test}"
        echo ""
        echo "Commands:"
        echo "  test    Run unit + integration tests (default)"
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

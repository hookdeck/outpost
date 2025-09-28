#!/bin/sh
set -e

# If any arguments are passed, delegate to outpost binary
# Examples: docker run hookdeck/outpost serve
#           docker run hookdeck/outpost migrate plan
#           docker run hookdeck/outpost --version
#           docker run hookdeck/outpost --help
if [ "$#" -gt 0 ]; then
    exec /usr/local/bin/outpost "$@"
fi

# Default behavior when no args: check migrations, then start server

echo "Checking database migrations..."
if ! /usr/local/bin/outpost migrate init --current --log-format=json; then
    echo ""
    echo "ERROR: Database migrations are pending."
    echo "Please run migrations before starting the server:"
    echo "  docker run --rm -it hookdeck/outpost migrate apply"
    echo ""
    echo "For help with migration commands and configuration:"
    echo "  docker run --rm hookdeck/outpost migrate --help"
    echo ""
    echo "Learn more about Outpost migration workflow at:"
    echo "  https://outpost.hookdeck.com/docs/guides/migration"
    echo ""
    exit 1
fi

echo "Starting Outpost server..."
exec /usr/local/bin/outpost serve

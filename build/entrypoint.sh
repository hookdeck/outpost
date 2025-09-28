#!/bin/sh
set -e

# If any arguments are passed, delegate to outpost binary
# Examples: docker run hookdeck/outpost serve
#           docker run hookdeck/outpost migrate plan
#           docker run hookdeck/outpost --version
if [ "$#" -gt 0 ]; then
    exec /usr/local/bin/outpost "$@"
fi

# Default behavior when no args: run migrations, then start server

echo "Running database migrations..."
/usr/local/bin/outpost migrate init --current --log-format=json

echo "Starting Outpost server..."
exec /usr/local/bin/outpost serve

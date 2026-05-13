#!/bin/sh
set -e

# If any arguments are passed, delegate to the outpost binary.
# Examples:
#   docker run hookdeck/outpost migrate apply
#   docker run hookdeck/outpost migrate plan
#   docker run hookdeck/outpost --version
if [ "$#" -gt 0 ]; then
    exec /usr/local/bin/outpost "$@"
fi

# Default behavior when no args: start the server. The server refuses to
# start if any SQL or Redis migrations are pending and prints a clear
# error telling the operator to run 'outpost migrate apply' first.
exec /usr/local/bin/outpost serve

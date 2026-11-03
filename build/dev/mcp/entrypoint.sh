#!/bin/sh
# Allow http callbacks to the compose network only, where the mcp-dev
# containers run: the subnet of the network `redis` (or its Dragonfly alias)
# is on.
set -e
peer=$(getent hosts redis 2>/dev/null | awk 'NR == 1 {print $1}')
cidr=""
if [ -n "$peer" ]; then
  dev=$(ip -4 route get "$peer" 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "dev") {print $(i + 1); exit}}')
  [ -n "$dev" ] && cidr=$(ip -4 -o route show dev "$dev" scope link 2>/dev/null | awk 'NR == 1 {print $1}')
fi
if [ -n "$cidr" ]; then
  DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST="${DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST:+$DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST,}$cidr"
else
  echo "warning: can't find the compose network; http callbacks to mcp-dev will be refused" >&2
fi
export DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST
exec sh ./build/dev/entrypoint.sh

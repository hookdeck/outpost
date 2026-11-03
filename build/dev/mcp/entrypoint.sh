#!/bin/sh
# Allow http callbacks to the host (mcp-dev receive/tour/check) only:
# every address host.docker.internal resolves to, as /32 or /128.
set -e
ips=$(getent ahosts host.docker.internal 2>/dev/null | awk '{print $1}' | sort -u)
[ -n "$ips" ] || ips=$(awk '$2 == "host.docker.internal" {print $1}' /etc/hosts | sort -u)
for ip in $ips; do
  case "$ip" in *:*) cidr="$ip/128" ;; *) cidr="$ip/32" ;; esac
  DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST="${DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST:+$DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST,}$cidr"
done
[ -n "$ips" ] || echo "warning: host.docker.internal doesn't resolve; http callbacks to the host will be refused" >&2
export DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST
exec sh ./build/dev/entrypoint.sh

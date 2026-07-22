#!/bin/sh
set -eu

base_compose=${1:-../docker-compose.yml}
pool_compose=${2:-docker-compose.proxy-pool.yml}

services=$(docker compose -p sub2api-v2 -f "$base_compose" -f "$pool_compose" config --services)
for service in $services; do
  case "$service" in
    app|postgres|redis|mihomo|proxy-controller) ;;
    *)
      echo "unexpected non-V2 service in compose: $service" >&2
      exit 1
      ;;
  esac
done

rendered=$(docker compose -p sub2api-v2 -f "$base_compose" -f "$pool_compose" config)
if printf '%s\n' "$rendered" | grep -Eq '(^|[[:space:]])(HTTP_PROXY|HTTPS_PROXY|ALL_PROXY):'; then
  echo "global proxy environment is forbidden in V2 app" >&2
  exit 1
fi

echo "V2 boundary verified"

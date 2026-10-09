#!/usr/bin/env bash
# Stop Astronomy Shop containers without deleting Compose volumes.
# Upstream `make stop` uses `down --volumes`; we intentionally avoid that.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${PERCH_ASTRONOMY_DEMO_DIR:-$ROOT/.demo}"
DOCKER_TIMEOUT_SECS="${ASTRONOMY_DOCKER_TIMEOUT_SECS:-15}"

if [[ ! -d "$DEST" ]]; then
  echo "No demo checkout at $DEST (nothing to stop)"
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to stop the demo" >&2
  exit 1
fi
if ! timeout "$DOCKER_TIMEOUT_SECS" docker info >/dev/null 2>&1; then
  echo "Docker daemon not responding within ${DOCKER_TIMEOUT_SECS}s; not running destructive stop" >&2
  exit 1
fi

cd "$DEST"
# Preserve volumes (postgres/valkey/kafka data). Orphans removed only.
docker compose --env-file .env --env-file .env.override \
  -f compose.yaml -f compose.full.yaml -f compose.observability.yaml -f compose.extras.yaml \
  down --remove-orphans

echo "Astronomy Shop stopped (volumes preserved)."
echo "To also wipe volumes, run upstream make stop inside the demo checkout deliberately."

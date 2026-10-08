#!/usr/bin/env bash
# Start Astronomy Shop. Default: make start-minimal (shop + observability, no Kafka).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${PERCH_ASTRONOMY_DEMO_DIR:-$ROOT/.demo}"
MODE="${ASTRONOMY_MODE:-minimal}"
DOCKER_TIMEOUT_SECS="${ASTRONOMY_DOCKER_TIMEOUT_SECS:-15}"

if [[ ! -d "$DEST" ]]; then
  echo "Demo checkout missing. Run: ./examples/astronomy-shop/scripts/clone.sh" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi
if ! timeout "$DOCKER_TIMEOUT_SECS" docker info >/dev/null 2>&1; then
  echo "Docker daemon not responding within ${DOCKER_TIMEOUT_SECS}s. Start/recover Docker Desktop and retry." >&2
  exit 1
fi

cd "$DEST"
case "$MODE" in
  minimal)
    TARGET=start-minimal
    ;;
  full)
    TARGET=start
    ;;
  agentic)
    TARGET=start-agentic
    ;;
  *)
    echo "Unknown ASTRONOMY_MODE=$MODE (use minimal|full|agentic)" >&2
    exit 1
    ;;
esac

echo "Starting Astronomy Shop mode=$MODE (make $TARGET) in $DEST"
if command -v make >/dev/null 2>&1; then
  make "$TARGET"
else
  echo "make not found; falling back to docker compose for minimal+o11y" >&2
  if [[ "$MODE" != "minimal" ]]; then
    echo "Without make, only ASTRONOMY_MODE=minimal is supported" >&2
    exit 1
  fi
  docker compose --env-file .env --env-file .env.override \
    -f compose.yaml -f compose.observability.yaml -f compose.extras.yaml \
    up --force-recreate --remove-orphans --detach
fi

echo "Web store: http://localhost:8080/"
echo "Jaeger:    http://localhost:8080/jaeger/ui/"
echo "Grafana:   http://localhost:8080/grafana/"

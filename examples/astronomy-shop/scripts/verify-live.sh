#!/usr/bin/env bash
# Live verification for a running Astronomy Shop. Not part of make verify / CI.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${PERCH_ASTRONOMY_DEMO_DIR:-$ROOT/.demo}"
BASE_URL="${ASTRONOMY_BASE_URL:-http://127.0.0.1:8080}"
# Jaeger UI is mounted under /jaeger/ui/; the services API is /jaeger/ui/api/services (not /jaeger/api/...).
JAEGER_SERVICES_URL="${ASTRONOMY_JAEGER_SERVICES_URL:-$BASE_URL/jaeger/ui/api/services}"
WAIT_SECS="${ASTRONOMY_VERIFY_WAIT_SECS:-180}"
DOCKER_TIMEOUT_SECS="${ASTRONOMY_DOCKER_TIMEOUT_SECS:-15}"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }

if ! command -v docker >/dev/null 2>&1; then
  fail "docker not installed"
fi
if ! timeout "$DOCKER_TIMEOUT_SECS" docker info >/dev/null 2>&1; then
  fail "Docker daemon not responding within ${DOCKER_TIMEOUT_SECS}s (start/recover Docker, then retry)"
fi

# Disk heuristic: upstream asks for several GB; warn under 4GiB free on /
avail_kb="$(df -k / | awk 'NR==2{print $4}')"
if [[ -n "$avail_kb" && "$avail_kb" -lt 4194304 ]]; then
  echo "WARN: less than ~4GiB free disk; image pulls may fail (avail_kb=$avail_kb)"
fi

echo "Waiting up to ${WAIT_SECS}s for frontend at $BASE_URL/"
deadline=$((SECONDS + WAIT_SECS))
frontend_ok=0
while (( SECONDS < deadline )); do
  if curl -sf --max-time 3 "$BASE_URL/" >/dev/null 2>&1; then
    frontend_ok=1
    break
  fi
  sleep 3
done
[[ "$frontend_ok" -eq 1 ]] || fail "frontend not reachable at $BASE_URL/"
ok "frontend reachable"

# Core containers expected in minimal mode
required_containers=(
  frontend-proxy frontend checkout cart product-catalog payment shipping
  currency recommendation ad email quote otel-collector jaeger
)
missing=0
for c in "${required_containers[@]}"; do
  status="$(docker inspect -f '{{.State.Status}}' "$c" 2>/dev/null || echo missing)"
  if [[ "$status" != "running" ]]; then
    echo "WARN: container $c status=$status"
    missing=1
  else
    ok "container $c running"
  fi
done
[[ "$missing" -eq 0 ]] || fail "one or more required containers not running"

# Optional: Jaeger service list (may be empty until traffic flows)
if curl -sf --max-time 5 "$JAEGER_SERVICES_URL" >/dev/null 2>&1; then
  services_json="$(curl -sf --max-time 10 "$JAEGER_SERVICES_URL" || true)"
  echo "Jaeger /api/services response (truncated):"
  echo "$services_json" | head -c 2000
  echo
  # Prefer seeing at least frontend or checkout after load-generator traffic
  if echo "$services_json" | grep -Eq '"frontend"|"checkout"|"frontend-proxy"'; then
    ok "Jaeger reports expected shop service name(s)"
  else
    echo "WARN: Jaeger reachable but expected service names not present yet (load may still be warming)"
  fi
else
  # Some Envoy routes use /jaeger/ui/ only; try direct container port if published
  if docker exec jaeger wget -qO- http://127.0.0.1:16686/api/services 2>/dev/null | head -c 2000; then
    echo
    ok "Jaeger API reachable inside container"
  else
    echo "WARN: could not query Jaeger services API; collector/UI may still be starting"
  fi
fi

# Document observed docker compose project services
echo "Running containers on network opentelemetry-demo (if present):"
docker network inspect opentelemetry-demo -f '{{range .Containers}}{{.Name}} {{end}}' 2>/dev/null || \
  docker ps --format '{{.Names}}' | head -40

ok "live verification checks completed"
if [[ -d "$DEST" ]]; then
  echo "Demo dir: $DEST"
fi
echo "Mapping reference: internal/pulse/astronomy/service-mapping.yaml"

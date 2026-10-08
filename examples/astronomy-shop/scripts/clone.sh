#!/usr/bin/env bash
# Shallow-clone OpenTelemetry Astronomy Shop at the pinned tag.
set -euo pipefail

PIN="${ASTRONOMY_DEMO_PIN:-3.1.0}"
UPSTREAM="${ASTRONOMY_DEMO_UPSTREAM:-https://github.com/open-telemetry/opentelemetry-demo.git}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${PERCH_ASTRONOMY_DEMO_DIR:-$ROOT/.demo}"
GIT_TIMEOUT_SECS="${ASTRONOMY_GIT_TIMEOUT_SECS:-120}"

run_git() {
  timeout "$GIT_TIMEOUT_SECS" git "$@"
}

if [[ -d "$DEST/.git" ]]; then
  echo "Demo already present at $DEST"
  run_git -C "$DEST" fetch --depth 1 origin "refs/tags/${PIN}:refs/tags/${PIN}" 2>/dev/null || \
    run_git -C "$DEST" fetch --depth 1 origin tag "$PIN"
  run_git -C "$DEST" checkout -q "tags/${PIN}" 2>/dev/null || run_git -C "$DEST" checkout -q "$PIN"
else
  echo "Cloning $UPSTREAM @ $PIN → $DEST"
  mkdir -p "$(dirname "$DEST")"
  run_git clone --depth 1 --branch "$PIN" "$UPSTREAM" "$DEST"
fi

# Ensure override file exists (upstream make targets touch it).
touch "$DEST/.env.override"

echo "OK: Astronomy Shop pin $PIN at $DEST"
echo "Next: ./examples/astronomy-shop/scripts/start.sh"

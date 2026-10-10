#!/usr/bin/env bash
# Record a SIMULATED deployment marker for Astronomy Shop correlation demos.
# This does NOT perform a real cloud/code deploy — it writes a Pulse change event.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SERVICE="${1:-}"
COMMIT="${2:-}"
ENV_NAME="${PERCH_CHANGE_ENV:-local}"
PULSE_DIR="${PERCH_PULSE_DIR:-$ROOT/examples/astronomy-shop/change/results}"

if [[ -z "$SERVICE" ]]; then
  echo "Usage: $0 <pulse-service-id> [commit-sha]" >&2
  echo "Example: $0 astronomy/local/payment abc123" >&2
  exit 2
fi

mkdir -p "$PULSE_DIR"
export PERCH_PULSE_DIR="$PULSE_DIR"
export PERCH_CHANGE_ENV="$ENV_NAME"

cd "$ROOT"
if [[ -n "$COMMIT" ]]; then
  exec go run ./examples/astronomy-shop/change/cmd/record "$SERVICE" "$COMMIT"
fi
exec go run ./examples/astronomy-shop/change/cmd/record "$SERVICE"

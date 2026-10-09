#!/usr/bin/env bash
# Demo-friendly wrapper for Astronomy Shop ground-truth fault scenarios.
# From perch-pulse repo root (or any subdirectory):
#
#   ./examples/astronomy-shop/scenarios/scenario.sh list
#   ./examples/astronomy-shop/scenarios/scenario.sh start error-payment
#   ./examples/astronomy-shop/scenarios/scenario.sh stop
#   ./examples/astronomy-shop/scenarios/scenario.sh status
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"
exec go run ./examples/astronomy-shop/scenarios/cmd/scenario "$@"

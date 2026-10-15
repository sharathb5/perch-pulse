# Perch Pulse — local verification
# `make verify` is the single gate; individual targets support iteration.
# Make stops on the first failing recipe (nonzero exit).

.PHONY: build web-build web-deps web-build-embed web-lint web-test web-e2e \
	go-fmt go-vet go-test provider-validate security \
	perch-regression pulse-integration databricks-integration-test \
	demo-preflight demo-up demo-status demo-fault demo-recover demo-down demo-test \
	verify test lint run

# Pin security scanners so the gate is reproducible across days.
GOSEC_VERSION ?= v2.29.0
GOVULNCHECK_VERSION ?= v1.8.0
# Must match go.mod `toolchain` so `go run` builds scanners with the same Go
# that analyzes this module (avoid host 1.24 + tool min 1.26 → go1.26.9 skew).
GO_TOOLCHAIN ?= go1.27.2

# --- Frontend ---------------------------------------------------------------

web-deps:
	cd web && npm ci

web-build-embed:
	cd web && npm run build:embed

web-lint:
	cd web && npm run lint

web-test:
	cd web && npm test

# Deterministic Playwright checks (Chromium). Not part of `verify` — separate CI job.
# Requires: npm ci, then `npx playwright install chromium` (check free disk first).
web-e2e:
	cd web && npm run test:e2e

# Legacy convenience: npm install + embed build (prefer web-deps for CI/verify).
web-build:
	cd web && npm install && npm run build:embed

# --- Go ---------------------------------------------------------------------

# Packages that embed web/dist require the Vite bundle first.
go-fmt:
	test -z "$$(gofmt -l .)"

go-vet:
	go vet ./...

go-test:
	go test ./...

# Original Perch regression (CLI/graph/probes/viz) — no Docker/Pulse required.
perch-regression:
	go test ./internal/cli/... ./internal/cli/test/... ./internal/graph/test/... ./internal/stackstatus/test/... ./internal/config/test/... -count=1

# Deterministic Pulse samples→detector→incident→API integration (no Docker).
pulse-integration:
	go test ./internal/pulse/pipeline/... ./internal/pulse/pipeline/test/... ./internal/pulse/live/test/... ./internal/pulse/telem/... -count=1

provider-validate:
	go test ./internal/providerspec/... -count=1

# Optional F3b offline Python tests (no Databricks/MLflow credentials).
# Not part of `make verify` — separate CI job to keep the Go gate lean.
# Uses a local venv under integrations/databricks/.venv (gitignored).
databricks-integration-test:
	cd integrations/databricks && \
		python3 -m venv .venv && \
		. .venv/bin/activate && \
		python -m pip install -q -U pip && \
		python -m pip install -q -e ".[test]" && \
		python -m pytest

security:
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) -quiet -exclude=G304 ./...
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# --- Demo recording (LIVE Astronomy Shop; see docs/demo-recording.md) -------
# Reuses existing shop/scenario/viz tooling. demo-up never injects a fault.
# demo-down stops only demo-owned perch viz; shop containers are preserved.

demo-preflight:
	./scripts/demo/recording.sh preflight

demo-up:
	./scripts/demo/recording.sh up

demo-status:
	./scripts/demo/recording.sh status

demo-fault:
	./scripts/demo/recording.sh fault

demo-recover:
	./scripts/demo/recording.sh recover

demo-down:
	./scripts/demo/recording.sh down

# Offline mocked orchestration checks (no Docker fault injection, no cloud).
demo-test:
	./scripts/demo/test_provenance.sh
	./scripts/demo/test_recording.sh
	python3 ./scripts/demo/test_payment_metrics.py

# --- Aggregate gate ---------------------------------------------------------

# Order matters: embed build before Go packages that import web.
verify: web-deps web-build-embed web-lint web-test go-fmt go-vet go-test provider-validate security demo-test

# --- Developer convenience --------------------------------------------------

build: web-build
	go build -o perch ./cmd/perch

test: go-test

lint: go-vet
	go fmt ./...

run:
	go run ./cmd/perch

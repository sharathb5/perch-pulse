# Perch Pulse — local verification
# `make verify` is the single gate; individual targets support iteration.
# Make stops on the first failing recipe (nonzero exit).

.PHONY: build web-build web-deps web-build-embed web-lint web-test \
	go-fmt go-vet go-test provider-validate security \
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

provider-validate:
	go test ./internal/providerspec/... -count=1

security:
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) -quiet -exclude=G304 ./...
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# --- Aggregate gate ---------------------------------------------------------

# Order matters: embed build before Go packages that import web.
verify: web-deps web-build-embed web-lint web-test go-fmt go-vet go-test provider-validate security

# --- Developer convenience --------------------------------------------------

build: web-build
	go build -o perch ./cmd/perch

test: go-test

lint: go-vet
	go fmt ./...

run:
	go run ./cmd/perch

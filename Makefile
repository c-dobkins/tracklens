VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build build-ui web web-test test cover fuzz golden lint vuln tidy docker

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/tracklens ./cmd/tracklens

# The binary with the dashboard embedded.
build-ui: web
	go build -trimpath -tags webui -ldflags "-X main.version=$(VERSION)" -o bin/tracklens ./cmd/tracklens

# Reinstall only when the lockfile changes.
web/node_modules/.package-lock.json: web/package-lock.json
	npm --prefix web ci --no-audit --no-fund

web: web/node_modules/.package-lock.json
	npm --prefix web run build

# End-to-end tests compile the binary with the dashboard, so build it first.
web-test: web
	npm --prefix web run lint
	npm --prefix web run typecheck
	npm --prefix web test
	npm --prefix web run e2e

# Set TEST_DATABASE_URL to include the PostgreSQL integration tests.
test:
	go test -race -shuffle=on ./...

cover:
	go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1

fuzz:
	go test -run='^$$' -fuzz=FuzzSplitDays -fuzztime=1m ./internal/report

# Rewrite the CLI golden files after an intended output change; review the diff.
golden:
	go test ./cmd/tracklens -run TestGolden -update

lint:
	golangci-lint run

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

tidy:
	go mod tidy -diff

docker:
	docker build --build-arg VERSION=$(VERSION) -t tracklens:$(VERSION) .

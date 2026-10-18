VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test cover fuzz golden lint vuln tidy docker

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/tracklens ./cmd/tracklens

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

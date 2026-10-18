VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test cover lint vuln docker
build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/tracklens ./cmd/tracklens

# Set TEST_DATABASE_URL to include the Postgres integration tests.
test:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

docker:
	docker build --build-arg VERSION=$(VERSION) -t tracklens:$(VERSION) .

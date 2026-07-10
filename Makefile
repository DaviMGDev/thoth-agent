.PHONY: all build test lint fmt clean coverage

BINARY := my-agent

all: fmt lint test build

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags="-X main.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/my-agent/

test:
	go test ./internal/... ./cmd/...

lint:
	go vet ./internal/... ./cmd/...
	golangci-lint run ./internal/... ./cmd/... 2>/dev/null || echo "golangci-lint not installed — skipping"

fmt:
	gofmt -s -w ./internal/ ./cmd/

coverage:
	go test -coverprofile=coverage.out ./internal/... ./cmd/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean:
	rm -f $(BINARY) $(BINARY).exe coverage.out coverage.html
	rm -rf *.test *.test.exe

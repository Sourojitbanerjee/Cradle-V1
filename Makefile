ifneq (,$(wildcard .env))
    include .env
    export
endif

.PHONY: all build test lint fmt run-api run-worker docker-up docker-down clean help

BIN_DIR := bin
API_BIN := $(BIN_DIR)/cradle-api
WORKER_BIN := $(BIN_DIR)/cradle-worker

all: lint test build

## build: Compiles the API server and Worker binaries
build:
	@echo "==> Building Cradle binaries..."
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-s -w" -o $(API_BIN) ./apps/api/cmd/server
	go build -ldflags="-s -w" -o $(WORKER_BIN) ./apps/worker/cmd/worker
	@echo "==> Binaries built in $(BIN_DIR)/"

## test: Runs all automated unit and integration tests with race detector
test:
	@echo "==> Running automated tests..."
	go test -race -v ./...

## lint: Checks formatting and runs go vet
lint:
	@echo "==> Running static analysis & vet..."
	go vet ./...
	@test -z "$$(gofmt -s -l . | grep -v '^vendor/')" || (echo "Unformatted files found:" && gofmt -s -l . && exit 1)
	@echo "==> Code formatting and static analysis clean!"

## fmt: Automatically formats all Go source files
fmt:
	@echo "==> Formatting code..."
	gofmt -s -w .

## run-api: Runs the API server locally
run-api:
	go run ./apps/api/cmd/server

## run-worker: Runs the Discovery Worker locally
run-worker:
	go run ./apps/worker/cmd/worker

## docker-up: Starts local Postgres and Redis dependencies
docker-up:
	docker compose -f deploy/docker-compose.yml up -d

## docker-down: Stops local Postgres and Redis dependencies
docker-down:
	docker compose -f deploy/docker-compose.yml down

## clean: Removes compiled binaries and temporary artifacts
clean:
	@echo "==> Cleaning build artifacts..."
	rm -rf $(BIN_DIR)

## help: Shows available make targets
help:
	@echo "Available commands:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':'

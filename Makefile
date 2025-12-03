.PHONY: build clean test docker-build docker-push run help

BINARY_NAME=harborbuddy
DOCKER_IMAGE=ghcr.io/mikeo/harborbuddy
VERSION=0.1.0

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary
	go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/harborbuddy

clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)
	go clean

test: ## Run tests
	go test -v ./...

docker-build: ## Build Docker image
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

docker-push: ## Push Docker image
	docker push $(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_IMAGE):latest

run: build ## Build and run locally
	./$(BINARY_NAME) --config examples/harborbuddy.yml

run-dry: build ## Build and run in dry-run mode
	./$(BINARY_NAME) --config examples/harborbuddy.yml --dry-run --once

fmt: ## Format Go code
	go fmt ./...

lint: ## Run linter
	golangci-lint run

tidy: ## Tidy go modules
	go mod tidy

deps: ## Download dependencies
	go mod download


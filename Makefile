BINARY_NAME := harborbuddy
IMAGE := ghcr.io/mikeo7/harborbuddy
VERSION ?= dev
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || printf 'unknown')
DATE ?= $(shell git show -s --format=%cI HEAD 2>/dev/null || printf 'unknown')
TAG ?= $(VERSION)
GOLANGCI_LINT_VERSION := v2.12.2
GOVULNCHECK_VERSION := v1.1.4
ACTIONLINT_VERSION := v1.7.12
ACTIONLINT_SHELLCHECK ?= ./test/shellcheck-pinned.sh
CONTAINER_ENGINE ?= $(shell if command -v docker >/dev/null 2>&1; then command -v docker; elif command -v podman >/dev/null 2>&1; then command -v podman; elif test -x /opt/podman/bin/podman; then printf /opt/podman/bin/podman; else printf docker; fi)

LDFLAGS := -s -w \
	-X github.com/MikeO7/HarborBuddy/internal/buildinfo.Version=$(VERSION) \
	-X github.com/MikeO7/HarborBuddy/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/MikeO7/HarborBuddy/internal/buildinfo.Date=$(DATE)

.PHONY: help build clean test test-cover test-race test-fuzz test-integration verify-local fmt fmt-check source-limits \
	vet lint vuln lint-actions lint-shell lint-docker lint-yaml lint-nongo tidy deps \
	docker-build docker-push run run-dry

help: ## Show available targets
	@printf 'Usage: make [target]\n\nAvailable targets:\n'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build a reproducible local binary
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/harborbuddy

clean: ## Remove local build artifacts
	rm -f $(BINARY_NAME) coverage.out coverage-packages.txt
	go clean -testcache

test: ## Run unit tests
	go test ./...

test-cover: ## Run tests with aggregate and per-package coverage gates
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@coverage="$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}')"; \
		awk -v coverage="$$coverage" 'BEGIN { if (coverage + 0 < 70) { printf "coverage %.1f%% is below 70%%\n", coverage; exit 1 } }'
	./test/check-coverage.sh

test-race: ## Run unit tests with the race detector
	go test -race ./...

test-fuzz: ## Fuzz security-sensitive parsers and identity logic briefly
	go test ./internal/docker -run='^$$' -fuzz=FuzzHelperDockerEnvironmentFiltersSecrets -fuzztime=3s
	go test ./internal/docker -run='^$$' -fuzz=FuzzPathRelevant -fuzztime=3s
	go test ./internal/selfupdate -run='^$$' -fuzz=FuzzDetectCurrentContainer -fuzztime=3s
	go test ./internal/selfupdate -run='^$$' -fuzz=FuzzUniquePrefixMatchRejectsShortIdentity -fuzztime=3s
	go test ./internal/updater -run='^$$' -fuzz=FuzzMatchesPattern -fuzztime=3s

test-integration: ## Run the Docker/Podman integration suite
	CONTAINER_ENGINE="$(CONTAINER_ENGINE)" ./test/integration.sh

verify-local: fmt-check source-limits vet lint vuln test-cover test-race test-fuzz build lint-nongo test-integration ## Run the comprehensive local verification suite

fmt: ## Format all repository Go source
	@gofmt -w $$(find . -type f -name '*.go' -not -path './.git/*' -not -path './.claude/*' -not -path './vendor/*')

fmt-check: ## Check all repository Go formatting
	@files="$$(find . -type f -name '*.go' -not -path './.git/*' -not -path './.claude/*' -not -path './vendor/*')"; \
		unformatted="$$(gofmt -l $$files)"; \
		test -z "$$unformatted" || { printf 'Go files require formatting:\n%s\n' "$$unformatted"; exit 1; }

source-limits: ## Enforce source file length limits
	./test/check-source-limits.sh

vet: ## Run go vet
	go vet ./...

lint: ## Run the pinned Go linter suite
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

vuln: ## Check reachable Go vulnerabilities
	GOVULNCHECK_VERSION=$(GOVULNCHECK_VERSION) ./test/run-govulncheck.sh

lint-actions: ## Lint GitHub Actions workflows
	CONTAINER_ENGINE="$(CONTAINER_ENGINE)" go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) -shellcheck "$(ACTIONLINT_SHELLCHECK)"

lint-shell: ## Lint maintained shell scripts
	CONTAINER_ENGINE="$(CONTAINER_ENGINE)" ./test/shellcheck-pinned.sh test/*.sh

lint-docker: ## Lint the Dockerfile with pinned Hadolint
	$(CONTAINER_ENGINE) run --rm --volume "$(CURDIR):/work:ro" \
		docker.io/hadolint/hadolint:v2.15.0-alpine@sha256:ddabce597257a5b466cc60878090de88b0fd191b84eb7652bddfe88ad192d0d5 \
		--config /work/.hadolint.yaml /work/Dockerfile

lint-yaml: ## Lint repository YAML files with pinned yamllint
	pipx run --spec yamllint==1.38.0 yamllint -c .yamllint.yml .github examples

lint-nongo: lint-actions lint-shell lint-docker lint-yaml ## Run all non-Go linters

tidy: ## Update go.mod and go.sum for development
	go mod tidy

deps: ## Download Go modules
	go mod download

docker-build: ## Build the local container image
	DOCKER_BUILDKIT=1 $(CONTAINER_ENGINE) build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t $(IMAGE):$(TAG) .

docker-push: ## Push the selected image tag
	$(CONTAINER_ENGINE) push $(IMAGE):$(TAG)

run: build ## Build and run with the example configuration
	./$(BINARY_NAME) --config examples/harborbuddy.yml

run-dry: build ## Run one local dry-run cycle
	./$(BINARY_NAME) --config examples/harborbuddy.yml --dry-run --once

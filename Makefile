BINARY  := hytale-runner
PKG     := github.com/voidcubedotgg/hytale-runner
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X $(PKG)/cmd.version=$(VERSION)
GO      ?= go

# Run a target inside the dev container instead of on the host: make dev-test
DC      := docker compose
DEVSVC  := dev

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary (version from git)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) .

.PHONY: run
run: ## Run the server (the `run` subcommand). Flags: make run ARGS="--log-level debug"
	$(GO) run -ldflags "$(LDFLAGS)" . run $(ARGS)

.PHONY: test
test: ## Run all tests
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run tests with the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Run tests and write coverage.out
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format the code
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-clean
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "not formatted:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: ci
ci: fmt-check vet test ## Run the checks CI should run

.PHONY: clean
clean: ## Remove build/coverage artifacts
	rm -rf $(BINARY) coverage.out dist

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml
	goreleaser check

.PHONY: release-snapshot
release-snapshot: ## Build a local snapshot release into dist/ (no publish)
	goreleaser release --snapshot --clean

.PHONY: dev-up
dev-up: ## Start the dev container + registry
	$(DC) up -d $(DEVSVC)

.PHONY: dev-down
dev-down: ## Stop the compose stack
	$(DC) down

.PHONY: dev-shell
dev-shell: ## Open a shell in the dev container
	$(DC) exec $(DEVSVC) bash

dev-%: ## Run any target inside the dev container, e.g. make dev-test
	$(DC) exec -T $(DEVSVC) make $*

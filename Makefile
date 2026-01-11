# ===================================
# dnsweaver - Makefile
# ===================================
# Declarative DNS synchronization from service discovery sources

.PHONY: all build clean test lint docker-build docker-run help dev

# ─────────────────────────────────────────────────────────────────────────────
# Configuration
# ─────────────────────────────────────────────────────────────────────────────

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod

# Binary names
BINARY_NAME := dnsweaver
CMD_PATH := ./cmd/dnsweaver

# Docker parameters
DOCKER_IMAGE := registry.bluewillows.net/root/dnsweaver
DOCKER_TAG := dev

# Build flags
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

# ─────────────────────────────────────────────────────────────────────────────
# Development
# ─────────────────────────────────────────────────────────────────────────────

## dev: Run in development mode with hot reload (requires Go)
dev:
	$(GOCMD) run $(CMD_PATH)

## dev-docker: Run using docker-compose for local development
dev-docker:
	docker compose -f docker-compose.dev.yml up --build

## dev-docker-down: Stop docker-compose development environment
dev-docker-down:
	docker compose -f docker-compose.dev.yml down -v

# ─────────────────────────────────────────────────────────────────────────────
# Build
# ─────────────────────────────────────────────────────────────────────────────

## all: Run lint, test, and build (default target)
all: lint test build

## build: Build the binary for current platform
build:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) $(CMD_PATH)

## build-linux: Build for Linux amd64 (for containers)
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 $(CMD_PATH)

## build-linux-arm64: Build for Linux arm64
build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 $(CMD_PATH)

## build-windows: Build for Windows
build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME).exe $(CMD_PATH)

## build-all: Build for all platforms
build-all: build-linux build-linux-arm64 build-windows

# ─────────────────────────────────────────────────────────────────────────────
# Quality
# ─────────────────────────────────────────────────────────────────────────────

## lint: Run golangci-lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "⚠️  golangci-lint not installed. Install with:"; \
		echo "    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

## lint-fix: Run golangci-lint with auto-fix
lint-fix:
	golangci-lint run --fix ./...

## fmt: Format code
fmt:
	$(GOCMD) fmt ./...

## vet: Run go vet
vet:
	$(GOCMD) vet ./...

## test: Run tests
test:
	$(GOTEST) -v -race ./...

## test-cover: Run tests with coverage report
test-cover:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## test-short: Run short tests only
test-short:
	$(GOTEST) -v -short ./...

# ─────────────────────────────────────────────────────────────────────────────
# Security
# ─────────────────────────────────────────────────────────────────────────────

## security: Run all security checks
security: vuln secrets

## vuln: Check for known vulnerabilities
vuln:
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "⚠️  govulncheck not installed. Install with:"; \
		echo "    go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	fi

## secrets: Scan for secrets with gitleaks
secrets:
	@if command -v gitleaks >/dev/null 2>&1; then \
		gitleaks detect --source . --verbose; \
	else \
		echo "⚠️  gitleaks not installed. Install from:"; \
		echo "    https://github.com/gitleaks/gitleaks#installing"; \
	fi

# ─────────────────────────────────────────────────────────────────────────────
# Dependencies
# ─────────────────────────────────────────────────────────────────────────────

## deps: Download dependencies
deps:
	$(GOMOD) download

## tidy: Tidy go.mod
tidy:
	$(GOMOD) tidy

## verify: Verify dependencies
verify:
	$(GOMOD) verify

# ─────────────────────────────────────────────────────────────────────────────
# Docker
# ─────────────────────────────────────────────────────────────────────────────

## docker-build: Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

## docker-build-clean: Build Docker image without cache
docker-build-clean:
	docker build --no-cache -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

## docker-run: Run Docker container locally
docker-run: docker-build
	docker run --rm -it \
		-v /var/run/docker.sock:/var/run/docker.sock:ro \
		-e DNSWEAVER_LOG_LEVEL=debug \
		-e TZ=America/Chicago \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

## docker-push: Push image to registry
docker-push:
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

# ─────────────────────────────────────────────────────────────────────────────
# Cleanup
# ─────────────────────────────────────────────────────────────────────────────

## clean: Remove build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-linux-amd64
	rm -f $(BINARY_NAME)-linux-arm64
	rm -f $(BINARY_NAME).exe
	rm -f coverage.out coverage.html

# ─────────────────────────────────────────────────────────────────────────────
# Tools
# ─────────────────────────────────────────────────────────────────────────────

## tools: Install development tools
tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo ""
	@echo "Install gitleaks separately: https://github.com/gitleaks/gitleaks#installing"

# ─────────────────────────────────────────────────────────────────────────────
# Help
# ─────────────────────────────────────────────────────────────────────────────

## help: Show this help
help:
	@echo "dnsweaver - Declarative DNS synchronization"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Development:"
	@grep -E '^## (dev|dev-docker)' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "Build:"
	@grep -E '^## (all|build)' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "Quality:"
	@grep -E '^## (lint|fmt|vet|test)' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "Security:"
	@grep -E '^## (security|vuln|secrets)' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "Docker:"
	@grep -E '^## docker' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "Other:"
	@grep -E '^## (deps|tidy|verify|clean|tools)' $(MAKEFILE_LIST) | sed 's/## /  /'

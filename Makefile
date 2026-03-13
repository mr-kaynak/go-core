# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Binary names
BINARY_API=go-core-api
BINARY_GRPC=go-core-grpc
BINARY_MIGRATE=go-core-migrate

# Build directories
BUILD_DIR=./bin
DIST_DIR=./dist

# Version info
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME?=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS=-ldflags="-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

# Docker parameters
DOCKER_REGISTRY?=ghcr.io/mr-kaynak
DOCKER_IMAGE_NAME?=go-core
DOCKER_TAG?=latest

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
NC=\033[0m # No Color

# Default target
.DEFAULT_GOAL := help

# PHONY targets
.PHONY: help init create clean deps build build-api build-grpc build-migrate run run-api run-grpc \
	test test-coverage test-integration test-e2e lint fmt vet \
	migrate migrate-up migrate-down migrate-status migrate-reset migrate-redo migrate-create \
	seed seed-clean \
	docker-build docker-build-api docker-push docker-push-api docker-up docker-down docker-logs docker-clean \
	proto swagger install-tools dev dev-full stop version

## help: Display this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^##' Makefile | sed 's/## /  /'

## init: Initialize a new project with custom module name
init:
	@if [ -z "$(NAME)" ]; then \
		echo "$(RED)Error: NAME parameter is required$(NC)"; \
		echo "Usage: make init NAME=github.com/yourname/project"; \
		exit 1; \
	fi
	@echo "$(GREEN)Initializing project with module: $(NAME)$(NC)"
	@./scripts/init-project.sh $(NAME)
	@echo "$(GREEN)Project initialized successfully!$(NC)"

## create: Create a new project from this boilerplate
create:
	@if [ -z "$(NAME)" ]; then \
		echo "$(RED)Error: NAME parameter is required$(NC)"; \
		echo "Usage: make create NAME=my-project"; \
		exit 1; \
	fi
	@echo "$(GREEN)Creating new project: $(NAME)$(NC)"
	@cp -r . ../$(NAME)
	@cd ../$(NAME) && rm -rf .git && git init
	@echo "$(GREEN)Project created at: ../$(NAME)$(NC)"
	@echo "$(YELLOW)Run 'cd ../$(NAME) && make init NAME=github.com/yourname/$(NAME)' to initialize$(NC)"

## clean: Clean build cache and binaries
clean:
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	@$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -rf $(DIST_DIR)
	@rm -rf tmp/
	@echo "$(GREEN)Clean complete!$(NC)"

## deps: Download and tidy dependencies
deps:
	@echo "$(YELLOW)Downloading dependencies...$(NC)"
	@$(GOMOD) download
	@$(GOMOD) tidy
	@echo "$(GREEN)Dependencies updated!$(NC)"

## build: Build all binaries
build: clean
	@echo "$(YELLOW)Building binaries...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_API) -v ./cmd/api
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_GRPC) -v ./cmd/grpc
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_MIGRATE) -v ./cmd/migrate
	@echo "$(GREEN)Build complete!$(NC)"

## build-api: Build API server binary
build-api:
	@echo "$(YELLOW)Building API server...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_API) -v ./cmd/api
	@echo "$(GREEN)API server built: $(BUILD_DIR)/$(BINARY_API)$(NC)"

## build-grpc: Build gRPC server binary
build-grpc:
	@echo "$(YELLOW)Building gRPC server...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_GRPC) -v ./cmd/grpc
	@echo "$(GREEN)gRPC server built: $(BUILD_DIR)/$(BINARY_GRPC)$(NC)"

## build-migrate: Build migration binary
build-migrate:
	@echo "$(YELLOW)Building migration tool...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_MIGRATE) -v ./cmd/migrate
	@echo "$(GREEN)Migration tool built: $(BUILD_DIR)/$(BINARY_MIGRATE)$(NC)"

## run: Run API server with hot reload
run:
	@if [ ! -f .air.toml ]; then \
		echo "$(YELLOW)Creating Air configuration...$(NC)"; \
		cp configs/.air.toml.example .air.toml 2>/dev/null || echo "[build]\n  cmd = \"go build -o ./tmp/main ./cmd/api\"\n  bin = \"tmp/main\"\n  include_ext = [\"go\", \"yaml\", \"yml\"]\n  exclude_dir = [\"tmp\", \"vendor\", \"node_modules\"]" > .air.toml; \
	fi
	@if command -v air > /dev/null; then \
		air; \
	elif [ -f "$$(go env GOPATH)/bin/air" ]; then \
		"$$(go env GOPATH)/bin/air"; \
	else \
		echo "$(YELLOW)Air not installed. Running without hot reload...$(NC)"; \
		$(GOCMD) run ./cmd/api; \
	fi

## run-api: Run API server
run-api:
	@echo "$(GREEN)Starting API server...$(NC)"
	@$(GOCMD) run ./cmd/api

## run-grpc: Run gRPC server
run-grpc:
	@echo "$(GREEN)Starting gRPC server...$(NC)"
	@$(GOCMD) run ./cmd/grpc

## test: Run all tests
test:
	@echo "$(YELLOW)Running tests...$(NC)"
	@$(GOTEST) -v -race -cover -short ./...
	@echo "$(GREEN)Tests complete!$(NC)"

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "$(YELLOW)Running tests with coverage...$(NC)"
	@$(GOTEST) -v -coverprofile=coverage.out ./...
	@$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

## test-integration: Run integration tests with testcontainers
test-integration:
	@echo "$(YELLOW)Running integration tests...$(NC)"
	@$(GOTEST) -v -tags=integration ./test/integration/...
	@echo "$(GREEN)Integration tests complete!$(NC)"

## test-e2e: Run end-to-end tests
test-e2e:
	@echo "$(YELLOW)Running E2E tests...$(NC)"
	@$(GOTEST) -v -tags=e2e ./test/e2e/...
	@echo "$(GREEN)E2E tests complete!$(NC)"

## lint: Run linter
lint:
	@echo "$(YELLOW)Running linter...$(NC)"
	@if command -v golangci-lint > /dev/null; then \
		$(GOLINT) run ./...; \
	else \
		echo "$(RED)golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)Linting complete!$(NC)"

## fmt: Format code
fmt:
	@echo "$(YELLOW)Formatting code...$(NC)"
	@$(GOFMT) -s -w .
	@$(GOCMD) fmt ./...
	@echo "$(GREEN)Formatting complete!$(NC)"

## vet: Run go vet
vet:
	@echo "$(YELLOW)Running go vet...$(NC)"
	@$(GOCMD) vet ./...
	@echo "$(GREEN)Vet complete!$(NC)"

## migrate: Run all pending migrations (alias: migrate-up)
migrate:
	@echo "$(YELLOW)Running database migrations...$(NC)"
	@$(GOCMD) run ./cmd/migrate up
	@echo "$(GREEN)Migrations complete!$(NC)"

## migrate-up: Apply all pending migrations
migrate-up: migrate

## migrate-down: Roll back the last migration
migrate-down:
	@echo "$(YELLOW)Rolling back last migration...$(NC)"
	@$(GOCMD) run ./cmd/migrate down
	@echo "$(GREEN)Rollback complete!$(NC)"

## migrate-status: Show migration status
migrate-status:
	@$(GOCMD) run ./cmd/migrate status

## migrate-reset: Roll back all migrations
migrate-reset:
	@echo "$(YELLOW)Resetting all migrations...$(NC)"
	@$(GOCMD) run ./cmd/migrate reset
	@echo "$(GREEN)Reset complete!$(NC)"

## migrate-redo: Roll back and re-apply the last migration
migrate-redo:
	@echo "$(YELLOW)Redoing last migration...$(NC)"
	@$(GOCMD) run ./cmd/migrate redo
	@echo "$(GREEN)Redo complete!$(NC)"

## migrate-create: Create a new migration file (NAME=migration_name)
migrate-create:
	@if [ -z "$(NAME)" ]; then \
		echo "$(RED)Error: NAME parameter is required$(NC)"; \
		echo "Usage: make migrate-create NAME=create_users_table"; \
		exit 1; \
	fi
	@echo "$(YELLOW)Creating migration: $(NAME)$(NC)"
	@$(GOCMD) run ./cmd/migrate create $(NAME)
	@echo "$(GREEN)Migration created!$(NC)"

## seed: Seed the database with test data
seed:
	@echo "$(YELLOW)Seeding database...$(NC)"
	@$(GOCMD) run ./cmd/seed
	@echo "$(GREEN)Database seeded!$(NC)"

## seed-clean: Drop all tables and reseed the database
seed-clean:
	@echo "$(YELLOW)Cleaning and reseeding database...$(NC)"
	@echo "$(RED)Warning: This will drop all tables!$(NC)"
	@$(GOCMD) run ./cmd/seed --clean
	@echo "$(GREEN)Database cleaned and reseeded!$(NC)"

## docker-build: Build Docker images for all targets
docker-build:
	@echo "$(YELLOW)Building Docker images...$(NC)"
	@docker buildx build --platform linux/amd64 --target api -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-api:$(DOCKER_TAG) --load .
	@docker buildx build --platform linux/amd64 --target grpc -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-grpc:$(DOCKER_TAG) --load .
	@docker buildx build --platform linux/amd64 --target migrate -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-migrate:$(DOCKER_TAG) --load .
	@echo "$(GREEN)Docker images built!$(NC)"

## docker-build-api: Build API Docker image only
docker-build-api:
	@echo "$(YELLOW)Building API image...$(NC)"
	@docker buildx build --platform linux/amd64 --target api -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-api:$(DOCKER_TAG) --load .
	@echo "$(GREEN)API image built!$(NC)"

## docker-push: Build and push all images to GHCR
docker-push:
	@echo "$(YELLOW)Building and pushing images to $(DOCKER_REGISTRY)...$(NC)"
	@docker buildx build --platform linux/amd64 --target api -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-api:$(DOCKER_TAG) --push .
	@docker buildx build --platform linux/amd64 --target grpc -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-grpc:$(DOCKER_TAG) --push .
	@docker buildx build --platform linux/amd64 --target migrate -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-migrate:$(DOCKER_TAG) --push .
	@echo "$(GREEN)All images pushed!$(NC)"

## docker-push-api: Build and push API image to GHCR
docker-push-api:
	@echo "$(YELLOW)Building and pushing API image...$(NC)"
	@docker buildx build --platform linux/amd64 --target api -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)-api:$(DOCKER_TAG) --push .
	@echo "$(GREEN)API image pushed!$(NC)"

## docker-up: Start all services with docker-compose
docker-up:
	@echo "$(YELLOW)Starting Docker services...$(NC)"
	@docker-compose up -d
	@echo "$(GREEN)Services started!$(NC)"
	@echo "$(YELLOW)Waiting for services to be healthy...$(NC)"
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		if docker-compose ps | grep -q "unhealthy\|starting"; then \
			sleep 2; \
		else \
			break; \
		fi; \
	done
	@docker-compose ps
	@echo ""
	@echo "$(GREEN)Services are running:$(NC)"
	@echo "  - Redis: localhost:6379"
	@echo "  - RabbitMQ: localhost:5672 (Management UI: http://localhost:15672)"
	@echo "  - Jaeger UI: http://localhost:16686"
	@echo "  - MailHog: http://localhost:8025"

## docker-down: Stop all services
docker-down:
	@echo "$(YELLOW)Stopping Docker services...$(NC)"
	@docker-compose down
	@echo "$(GREEN)Services stopped!$(NC)"

## docker-logs: Show logs from docker-compose services
docker-logs:
	@docker-compose logs -f

## docker-clean: Stop services and remove volumes
docker-clean:
	@echo "$(YELLOW)Cleaning Docker services and volumes...$(NC)"
	@docker-compose down -v
	@echo "$(GREEN)Clean complete!$(NC)"

## proto: Generate gRPC code from proto files
proto:
	@echo "$(YELLOW)Generating gRPC code...$(NC)"
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/*.proto
	@echo "$(GREEN)gRPC code generated!$(NC)"

## swagger: Generate OpenAPI 3.1 documentation
swagger:
	@echo "$(YELLOW)Generating Swagger documentation...$(NC)"
	@go run github.com/swaggo/swag/v2/cmd/swag@latest init -d ./cmd/api,./internal -g main.go -o ./docs --parseDependency --parseInternal
	@echo "$(YELLOW)Upgrading to OpenAPI 3.1...$(NC)"
	@npx -y @scalar/cli document upgrade docs/swagger.json --output docs/openapi.json
	@npx -y @scalar/cli document upgrade docs/swagger.yaml --output docs/openapi.yaml
	@rm -f docs/swagger.json docs/swagger.yaml
	@echo "$(GREEN)OpenAPI 3.1 documentation generated!$(NC)"

## install-tools: Install development tools
install-tools:
	@echo "$(YELLOW)Installing development tools...$(NC)"
	@go install github.com/air-verse/air@v1.64.5
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v2.11.3
	@go install github.com/swaggo/swag/cmd/swag@v1.16.6
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.9
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
	@go install github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1
	@echo "$(GREEN)Tools installed!$(NC)"

## dev: Start development environment (docker services + API)
dev: docker-up
	@echo "$(YELLOW)Waiting for services to be ready...$(NC)"
	@sleep 5
	@echo "$(GREEN)Starting API server...$(NC)"
	@make run

## dev-full: Start full development environment (all services)
dev-full: docker-up
	@echo "$(YELLOW)Starting all services...$(NC)"
	@make run-api &
	@make run-grpc &
	@echo "$(GREEN)All services started!$(NC)"

## stop: Stop all running services
stop:
	@echo "$(YELLOW)Stopping services...$(NC)"
	@pkill -f "$(BINARY_API)" || true
	@pkill -f "$(BINARY_GRPC)" || true
	@pkill -f "air" || true
	@echo "$(GREEN)Services stopped!$(NC)"

## version: Show version information
version:
	@echo "Go-Core Boilerplate"
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Built:   $(BUILD_TIME)"
	@echo "Go:      $(shell go version)"

# Catch-all target
%:
	@echo "$(RED)Unknown target: $@$(NC)"
	@echo "Run 'make help' to see available targets"
# nGX Monorepo Makefile
# Usage: make <target> [name=migration_name]

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ============================================================
# Variables
# ============================================================

SERVICES := api auth inbox email-pipeline event-dispatcher webhook-service scheduler search embedder
BIN_DIR   := bin
DIST_DIR  := dist

# Lambda build configuration
LAMBDA_NAMES := authorizer orgs auth inboxes threads messages drafts webhooks search domains \
                ws_connect ws_disconnect email_inbound email_outbound \
                event_dispatcher_webhook event_dispatcher_ws embedder ses_events scheduler_drafts

S3_ARTIFACTS_BUCKET ?= $(shell cd terraform && terraform output -raw s3_bucket_artifacts 2>/dev/null || echo "ngx-prod-artifacts")
AWS_REGION ?= us-east-1

# Docker compose
DC        := docker compose

# Go tools
GOLANGCI  := golangci-lint
DB_ENV    := env DATABASE_URL=$(shell grep '^DATABASE_URL=' .env | cut -d= -f2-)
MIGRATE   := $(DB_ENV) go run ./tools/migrate

# Build info
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -ldflags="-X main.gitCommit=$(GIT_COMMIT) -X main.buildTime=$(BUILD_TIME) -s -w"

# ============================================================
# Help
# ============================================================

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	/^[a-zA-Z_\-]+:.*?##/ { printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ============================================================
# Docker Compose
# ============================================================

.PHONY: up
up: ## Start all infrastructure services (detached)
	$(DC) up -d
	@echo "Waiting for postgres to be healthy..."
	@until docker compose exec -T postgres pg_isready -U agentmail -d agentmail > /dev/null 2>&1; do sleep 1; done
	@echo "All services are up."

.PHONY: down
down: ## Stop all infrastructure services
	$(DC) down

.PHONY: down-volumes
down-volumes: ## Stop all services and remove volumes (destructive)
	$(DC) down -v

.PHONY: logs
logs: ## Tail logs from all infrastructure services
	$(DC) logs -f

.PHONY: logs-SERVICE
logs-%: ## Tail logs for a specific docker compose service (e.g. make logs-postgres)
	$(DC) logs -f $*

.PHONY: ps
ps: ## Show status of all docker compose services
	$(DC) ps

# ============================================================
# Database Migrations
# ============================================================

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	$(MIGRATE) up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	$(MIGRATE) down 1

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(MIGRATE) status

.PHONY: migrate-create
migrate-create: ## Create a new migration file: make migrate-create name=add_users_table
ifndef name
	$(error name is required. Usage: make migrate-create name=<migration_name>)
endif
	$(MIGRATE) create $(name)

.PHONY: migrate-reset
migrate-reset: ## Roll back ALL migrations (destructive)
	$(MIGRATE) down 0

# ============================================================
# Build
# ============================================================

.PHONY: build
build: $(addprefix build-,$(SERVICES)) ## Build all services

.PHONY: $(addprefix build-,$(SERVICES))
build-api: ## Build the api service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/api ./services/api/cmd/api

build-auth: ## Build the auth service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/auth ./services/auth/cmd/auth

build-inbox: ## Build the inbox service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/inbox ./services/inbox/cmd/inbox

build-email-pipeline: ## Build the email-pipeline service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/email-pipeline ./services/email-pipeline/cmd/email-pipeline

build-event-dispatcher: ## Build the event-dispatcher service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/event-dispatcher ./services/event-dispatcher/cmd/event-dispatcher

build-webhook-service: ## Build the webhook-service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/webhook-service ./services/webhook-service/cmd/webhook-service

build-scheduler: ## Build the scheduler service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/scheduler ./services/scheduler/cmd/scheduler

build-search: ## Build the search service binary
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/search ./services/search/cmd/search

# ============================================================
# Lambda Build & Deploy
# ============================================================

.PHONY: build-lambda-%
build-lambda-%: ## Build a single Lambda function (arm64 Linux): make build-lambda-authorizer
	@echo "Building Lambda: $*"
	@mkdir -p dist/lambdas/$*
	cd lambdas/$* && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o ../../dist/lambdas/$*/bootstrap .
	cd dist/lambdas/$* && zip -j ../../../dist/lambdas/$*.zip bootstrap

.PHONY: build-lambdas
build-lambdas: $(addprefix build-lambda-,$(LAMBDA_NAMES)) ## Build all Lambda functions
	@echo "All Lambdas built successfully"

.PHONY: deploy-lambda-%
deploy-lambda-%: build-lambda-% ## Build and deploy a single Lambda to AWS: make deploy-lambda-authorizer
	@echo "Deploying Lambda: $*"
	@FNAME=$$(echo "ngx-prod-$*" | tr '_' '-'); \
	aws s3 cp dist/lambdas/$*.zip s3://$(S3_ARTIFACTS_BUCKET)/lambdas/$*.zip --region $(AWS_REGION); \
	aws lambda update-function-code \
		--function-name $$FNAME \
		--s3-bucket $(S3_ARTIFACTS_BUCKET) \
		--s3-key lambdas/$*.zip \
		--region $(AWS_REGION) \
		--architectures arm64

.PHONY: deploy-lambdas
deploy-lambdas: $(addprefix deploy-lambda-,$(LAMBDA_NAMES)) ## Build and deploy all Lambda functions to AWS
	@echo "All Lambdas deployed successfully"

# ============================================================
# Tests
# ============================================================

.PHONY: test
test: ## Run all tests across all modules
	@echo "Running tests..."
	go test -race -count=1 ./pkg/... ./services/api/... ./services/auth/... \
		./services/inbox/... ./services/email-pipeline/... ./services/event-dispatcher/... \
		./services/webhook-service/... ./services/scheduler/... ./services/search/... \
		./services/embedder/...

.PHONY: test-coverage
test-coverage: ## Run all tests with coverage report
	@echo "Running tests with coverage..."
	go test -race -count=1 -coverprofile=coverage.out ./pkg/... ./services/api/... ./services/auth/... \
		./services/inbox/... ./services/email-pipeline/... ./services/event-dispatcher/... \
		./services/webhook-service/... ./services/scheduler/... ./services/search/... \
		./services/embedder/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"

.PHONY: test-coverage-unit
test-coverage-unit: ## Run coverage on unit-testable packages only (excludes infra/Kafka/DB-dependent code)
	@echo "Running unit coverage (pure + mockable packages)..."
	go test -race -count=1 -coverprofile=coverage-unit.out \
		./pkg/auth/... ./pkg/crypto/... ./pkg/pagination/... ./pkg/validate/... \
		./pkg/events/... ./pkg/embedder/... ./pkg/mime/... \
		./services/auth/service/... ./services/auth/handlers/...
	go tool cover -html=coverage-unit.out -o coverage-unit.html
	@go tool cover -func=coverage-unit.out | grep "^total"
	@echo "Coverage report written to coverage-unit.html"

test-api: ## Run tests for the api service
	go test -race -count=1 -v ./services/api/...

test-auth: ## Run tests for the auth service
	go test -race -count=1 -v ./services/auth/...

test-inbox: ## Run tests for the inbox service
	go test -race -count=1 -v ./services/inbox/...

test-email-pipeline: ## Run tests for the email-pipeline service
	go test -race -count=1 -v ./services/email-pipeline/...

test-event-dispatcher: ## Run tests for the event-dispatcher service
	go test -race -count=1 -v ./services/event-dispatcher/...

test-webhook-service: ## Run tests for the webhook-service
	go test -race -count=1 -v ./services/webhook-service/...

test-scheduler: ## Run tests for the scheduler service
	go test -race -count=1 -v ./services/scheduler/...

test-search: ## Run tests for the search service
	go test -race -count=1 -v ./services/search/...

test-pkg: ## Run tests for shared packages
	go test -race -count=1 -v ./pkg/...

# ============================================================
# Code Quality
# ============================================================

.PHONY: lint
lint: ## Run golangci-lint across all modules
	$(GOLANGCI) run ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	$(GOLANGCI) run --fix ./...

.PHONY: fmt
fmt: ## Format all Go source files with gofmt
	@echo "Formatting Go files..."
	@gofmt -l -w $$(find . -name '*.go' -not -path './vendor/*')
	@echo "Done."

.PHONY: vet
vet: ## Run go vet across all modules
	go vet ./pkg/... ./services/... ./tools/...

.PHONY: generate
generate: ## Run go generate across all packages
	go generate ./...

# ============================================================
# Module Management
# ============================================================

.PHONY: tidy
tidy: ## Sync go.work and tidy all modules
	go work sync
	@for mod in pkg lambdas services/api services/auth services/inbox services/email-pipeline \
		services/event-dispatcher services/webhook-service services/scheduler services/search \
		services/embedder tools/migrate; do \
		echo "Tidying $$mod..."; \
		(cd $$mod && go mod tidy); \
	done
	@echo "All modules tidied."

# ============================================================
# Docker Image Builds
# ============================================================

IMAGE_REGISTRY ?= ghcr.io/nyang64/nGX
IMAGE_TAG      ?= $(GIT_COMMIT)

.PHONY: docker-build
docker-build: $(addprefix docker-build-,$(SERVICES)) ## Build all Docker images

docker-build-%: ## Build Docker image for a specific service
	docker build \
		--build-arg SERVICE=$* \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(IMAGE_REGISTRY)/$*:$(IMAGE_TAG) \
		-t $(IMAGE_REGISTRY)/$*:latest \
		-f services/$*/Dockerfile \
		.

.PHONY: docker-push
docker-push: ## Push all Docker images to registry
	@for svc in $(SERVICES); do \
		docker push $(IMAGE_REGISTRY)/$$svc:$(IMAGE_TAG); \
		docker push $(IMAGE_REGISTRY)/$$svc:latest; \
	done

# ============================================================
# Local Development
# ============================================================

.PHONY: dev-api
dev-api: ## Run the api service locally with hot-reload (requires air)
	@which air > /dev/null 2>&1 || (echo "Installing air..." && go install github.com/air-verse/air@latest)
	cd services/api && air -c .air.toml

.PHONY: dev-auth
dev-auth: ## Run the auth service locally
	go run ./services/auth/cmd/auth

.PHONY: dev-inbox
dev-inbox: ## Run the inbox service locally
	go run ./services/inbox/cmd/inbox

.PHONY: dev-email-pipeline
dev-email-pipeline: ## Run the email-pipeline service locally
	go run ./services/email-pipeline/cmd/email-pipeline

.PHONY: dev-webhook-service
dev-webhook-service: ## Run the webhook-service locally
	go run ./services/webhook-service/cmd/webhook-service

.PHONY: dev-scheduler
dev-scheduler: ## Run the scheduler service locally
	go run ./services/scheduler/cmd/scheduler

.PHONY: dev-search
dev-search: ## Run the search service locally
	go run ./services/search/cmd/search

# ============================================================
# Clean
# ============================================================

.PHONY: clean
clean: ## Remove build artifacts
	@rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out coverage.html
	@find . -name '*.test' -delete
	@echo "Cleaned."

.PHONY: clean-docker
clean-docker: ## Remove all local Docker images for this project
	@for svc in $(SERVICES); do \
		docker rmi -f $(IMAGE_REGISTRY)/$$svc:latest 2>/dev/null || true; \
	done

# ============================================================
# Setup
# ============================================================

.PHONY: bootstrap
bootstrap: ## Create the first org and admin API key (run once after migrate-up)
	$(DB_ENV) go run ./tools/bootstrap $(if $(org),-org "$(org)") $(if $(slug),-slug "$(slug)")

.PHONY: setup
setup: ## Bootstrap local development environment
	@echo "Installing Go tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/air-verse/air@latest
	@echo "Copying .env.example to .env..."
	@test -f .env || cp .env.example .env
	@echo "Starting infrastructure..."
	$(MAKE) up
	@echo "Running migrations..."
	$(MAKE) migrate-up
	@echo "Setup complete. Edit .env as needed."

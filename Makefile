# nGX Monorepo Makefile

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ============================================================
# Variables
# ============================================================

# Lambda build configuration
LAMBDA_NAMES := authorizer orgs auth inboxes threads messages drafts webhooks search domains \
                ws_connect ws_disconnect email_inbound email_outbound \
                event_dispatcher_webhook event_dispatcher_ws embedder ses_events scheduler_drafts

DIST_DIR  := dist

S3_ARTIFACTS_BUCKET ?= $(shell cd terraform && terraform output -raw s3_bucket_artifacts 2>/dev/null || echo "ngx-prod-artifacts")
AWS_REGION ?= us-east-1

# Go tools
GOLANGCI  := golangci-lint
# DATABASE_URL: prefer .env.outputs (post-deploy), fall back to .env (local override)
DB_ENV    := env DATABASE_URL=$(shell grep '^DATABASE_URL=' .env.outputs 2>/dev/null | cut -d= -f2- || grep '^DATABASE_URL=' .env 2>/dev/null | cut -d= -f2-)
MIGRATE   := $(DB_ENV) go run ./tools/migrate

# Build info
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# ============================================================
# Help
# ============================================================

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	/^[a-zA-Z_\-]+:.*?##/ { printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ============================================================
# Lambda Build & Deploy
# ============================================================

.PHONY: build-lambda-%
build-lambda-%: ## Build a single Lambda (arm64 Linux): make build-lambda-authorizer
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
# Database Migrations
# ============================================================

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations (requires DATABASE_URL via SSM tunnel — see .env)
	$(MIGRATE) up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	$(MIGRATE) down 1

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(MIGRATE) status

.PHONY: migrate-create
migrate-create: ## Create a new migration file pair: make migrate-create name=add_users_table
ifndef name
	$(error name is required. Usage: make migrate-create name=<migration_name>)
endif
	$(MIGRATE) create $(name)

.PHONY: migrate-reset
migrate-reset: ## Roll back ALL migrations (destructive)
	$(MIGRATE) down 0

# ============================================================
# Tests
# ============================================================

.PHONY: test
test: ## Run unit tests across pkg/ and lambdas/
	@echo "Running tests..."
	go test -race -count=1 ./pkg/... ./lambdas/...

.PHONY: test-integration
test-integration: ## Run integration tests against the deployed AWS stack (requires source loadenv.sh first)
	@echo "Running integration tests..."
	go test -race -count=1 -v -timeout 120s ./tests/integration/...

.PHONY: test-coverage
test-coverage: ## Run unit tests with HTML coverage report
	@echo "Running tests with coverage..."
	go test -race -count=1 -coverprofile=coverage.out ./pkg/... ./lambdas/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"

.PHONY: test-pkg
test-pkg: ## Run tests for shared packages only
	go test -race -count=1 -v ./pkg/...

# ============================================================
# Code Quality
# ============================================================

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI) run ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	$(GOLANGCI) run --fix ./...

.PHONY: fmt
fmt: ## Format all Go source files
	@gofmt -l -w $$(find . -name '*.go' -not -path './vendor/*')

.PHONY: vet
vet: ## Run go vet
	go vet ./pkg/... ./lambdas/... ./tools/...

.PHONY: generate
generate: ## Run go generate
	go generate ./...

# ============================================================
# Module Management
# ============================================================

.PHONY: tidy
tidy: ## Sync go.work and tidy all modules
	go work sync
	@for mod in pkg lambdas tests/integration tools/bootstrap tools/migrate; do \
		echo "Tidying $$mod..."; \
		(cd $$mod && go mod tidy); \
	done
	@echo "All modules tidied."

# ============================================================
# Bootstrap & Setup
# ============================================================

.PHONY: bootstrap
bootstrap: ## Create the first org and admin API key (run once after migrate-up)
	$(DB_ENV) go run ./tools/bootstrap $(if $(org),-org "$(org)") $(if $(slug),-slug "$(slug)")

.PHONY: setup
setup: ## Install Go tools and print deployment instructions
	@echo "Installing Go tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Copying .env.example to .env (if not present)..."
	@test -f .env || cp .env.example .env
	@echo ""
	@echo "Next steps:"
	@echo "  1. Edit .env and fill in TF_VAR_* values"
	@echo "  2. source loadenv.sh && AWS_PROFILE=<profile> terraform -chdir=terraform apply"
	@echo "  3. scripts/sync-env.sh        # generates .env.outputs"
	@echo "  4. source loadenv.sh          # picks up endpoints and DATABASE_URL"
	@echo "  5. make migrate-up            # run via SSM tunnel (see .env for command)"
	@echo "  6. make bootstrap org='My Org' slug='my-org'"

# ============================================================
# Clean
# ============================================================

.PHONY: clean
clean: ## Remove build artifacts
	@rm -rf $(DIST_DIR) coverage.out coverage.html
	@find . -name '*.test' -delete
	@echo "Cleaned."

SHELL := /bin/bash
.DEFAULT_GOAL := help

-include .env
export

TEST_DATABASE_URL ?= postgres://lead:lead@localhost:5432/leadintel_test?sslmode=disable

.PHONY: help stack logs up down db api ai web openapi openapi-check proto proto-check tools \
	test test-go test-ai test-web lint record-websites

# Pinned code generators, installed into .bin/ (git-ignored) on first use.
BIN := $(CURDIR)/.bin
BUF_VERSION := v1.75.0
PROTOC_GEN_GO_VERSION := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-10s %s\n", $$1, $$2}'

stack: ## Build and start the full stack in Docker (http://localhost:5173)
	docker compose up --build -d --wait

logs: ## Follow the logs of all containers
	docker compose logs -f

up: ## Start only Postgres in Docker (for host-run services)
	docker compose up -d --wait postgres

down: ## Stop all containers
	docker compose down

db: up ## Create the test database if missing
	@docker compose exec -T postgres psql -U $${POSTGRES_USER:-lead} -d $${POSTGRES_DB:-leadintel} -tAc \
		"SELECT 1 FROM pg_database WHERE datname = 'leadintel_test'" | grep -q 1 || \
		docker compose exec -T postgres createdb -U $${POSTGRES_USER:-lead} leadintel_test

api: ## Run the Go API on the host
	cd backend && go run ./cmd/api

ai: ## Run the Python AI service on the host
	cd ai && uv run python -m lead_ai.main

web: ## Run the frontend dev server (proxies /api to :8080)
	cd frontend && npm run dev

openapi: ## Regenerate Go and TypeScript code from api/openapi.yaml
	cd backend && go generate ./internal/api/...
	cd frontend && npm run gen:api

openapi-check: openapi ## Fail if generated API code is out of date
	@git diff --exit-code -- backend/internal/api frontend/src/lib/api/gen \
		|| (echo "Generated API code is stale: run 'make openapi' and commit the result." && exit 1)
	@test -z "$$(git ls-files --others --exclude-standard -- backend/internal/api frontend/src/lib/api/gen)" \
		|| (echo "Untracked generated files: run 'make openapi' and commit the result." && exit 1)

tools: $(BIN)/buf $(BIN)/protoc-gen-go $(BIN)/protoc-gen-go-grpc ## Install pinned code generators into .bin/

$(BIN)/buf:
	GOBIN=$(BIN) go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
$(BIN)/protoc-gen-go:
	GOBIN=$(BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
$(BIN)/protoc-gen-go-grpc:
	GOBIN=$(BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

proto: tools ## Regenerate gRPC code (Go + Python) from proto/
	$(BIN)/buf lint
	rm -rf backend/gen ai/src/lead_ai/gen
	PATH="$(BIN):$$PATH" $(BIN)/buf generate
	# Python: the include mapping makes generated imports absolute within lead_ai.gen.
	cd ai && uv run --group dev python -m grpc_tools.protoc -Ilead_ai/gen=../proto \
		--python_out=src --pyi_out=src --grpc_python_out=src \
		../proto/leadintel/analysis/v1/analysis.proto

proto-check: proto ## Fail if generated gRPC code is out of date
	@git diff --exit-code -- backend/gen ai/src/lead_ai/gen \
		|| (echo "Generated gRPC code is stale: run 'make proto' and commit the result." && exit 1)
	@test -z "$$(git ls-files --others --exclude-standard -- backend/gen ai/src/lead_ai/gen)" \
		|| (echo "Untracked generated files: run 'make proto' and commit the result." && exit 1)

record-websites: ## Record real homepages as local test fixtures (git-ignored; SPEC.md §9.7)
	cd ai && uv run python scripts/record_websites.py

test: test-go test-ai test-web ## Run all tests

test-go: db ## Go unit + store integration tests
	# -p 1: store tests in several packages share (and truncate) the test database.
	cd backend && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -p 1 ./...

test-ai: ## Python unit tests (offline: fake LLM, website fixtures)
	cd ai && uv run pytest -q

test-web: ## Frontend type check and unit tests
	cd frontend && npm run check && npm test

lint: openapi-check proto-check ## Static checks (including generated-code freshness)
	cd backend && go vet ./...
	cd ai && uv run ruff check . && uv run ruff format --check .
	cd frontend && npm run check

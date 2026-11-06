SHELL := /bin/bash
.DEFAULT_GOAL := help

-include .env
export

TEST_DATABASE_URL ?= postgres://lead:lead@localhost:5432/leadintel_test?sslmode=disable

.PHONY: help up down db api web openapi openapi-check test test-go test-web lint

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-10s %s\n", $$1, $$2}'

up: ## Start infrastructure (Postgres) in Docker
	docker compose up -d --wait postgres

down: ## Stop all containers
	docker compose down

db: up ## Create the test database if missing
	@docker compose exec -T postgres psql -U $${POSTGRES_USER:-lead} -d $${POSTGRES_DB:-leadintel} -tAc \
		"SELECT 1 FROM pg_database WHERE datname = 'leadintel_test'" | grep -q 1 || \
		docker compose exec -T postgres createdb -U $${POSTGRES_USER:-lead} leadintel_test

api: ## Run the Go API on the host
	cd backend && go run ./cmd/api

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

test: test-go test-web ## Run all tests

test-go: db ## Go unit + store integration tests
	cd backend && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./...

test-web: ## Frontend type check
	cd frontend && npm run check

lint: openapi-check ## Static checks (including generated-code freshness)
	cd backend && go vet ./...
	cd frontend && npm run check

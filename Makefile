SHELL := /bin/bash
.DEFAULT_GOAL := help

-include .env
export

TEST_DATABASE_URL ?= postgres://lead:lead@localhost:5432/leadintel_test?sslmode=disable

.PHONY: help up down db api web test test-go test-web lint

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

test: test-go test-web ## Run all tests

test-go: db ## Go unit + store integration tests
	cd backend && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./...

test-web: ## Frontend type check
	cd frontend && npm run check

lint: ## Static checks
	cd backend && go vet ./...
	cd frontend && npm run check

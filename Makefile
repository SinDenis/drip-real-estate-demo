SHELL := /bin/bash
.DEFAULT_GOAL := help

PNPM := npx --yes pnpm@11.25.0
DOCKER ?= $(shell if command -v docker >/dev/null 2>&1; then command -v docker; elif [ -x /Applications/Docker.app/Contents/Resources/bin/docker ]; then printf /Applications/Docker.app/Contents/Resources/bin/docker; else printf docker; fi)
COMPOSE := "$(DOCKER)" compose --file compose.yaml
BACKEND_MAKE := $(MAKE) --no-print-directory --directory=backend
FRONTEND_MAKE := $(MAKE) --no-print-directory --directory=frontend

override FILE := $(value FILE)
override KEY := $(value KEY)
override QUERY := $(value QUERY)
override SERVICE := $(value SERVICE)
export FILE KEY QUERY SERVICE

.PHONY: help bootstrap generate test test-backend test-frontend lint architecture coverage \
	coverage-backend coverage-frontend build verify-fast verify e2e dev-up dev-down \
	doctor logs logs-service db-shell db-query s3-ls s3-get s3-put

help: ## Show available development commands.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

bootstrap: ## Install pinned frontend dependencies.
	$(FRONTEND_MAKE) bootstrap

generate: ## Regenerate Go and TypeScript code from OpenAPI.
	$(BACKEND_MAKE) generate
	$(FRONTEND_MAKE) generate

test: test-backend test-frontend ## Run native unit and contract tests.
	bash tests/eiszhs_contract_test.sh
	bash tests/eiszhs-probe_test.sh
	bash tests/local_commands_test.sh

test-backend: ## Run Go unit tests.
	$(BACKEND_MAKE) test

test-frontend: ## Run TypeScript and React unit tests.
	$(FRONTEND_MAKE) test

lint: ## Run Go, TypeScript, formatting, and shell checks.
	$(BACKEND_MAKE) lint
	$(FRONTEND_MAKE) lint
	bash -n scripts/eiszhs-probe scripts/check-go-coverage scripts/run-e2e tests/fixtures/docker-probe tests/*.sh

architecture: ## Enforce backend and frontend dependency boundaries.
	$(BACKEND_MAKE) architecture
	$(FRONTEND_MAKE) architecture

coverage: coverage-backend coverage-frontend ## Enforce all 90 percent coverage gates.

coverage-backend: ## Enforce 90 percent statement coverage for the isolated Go core.
	$(BACKEND_MAKE) coverage

coverage-frontend: ## Enforce frontend line, statement, function, and branch coverage.
	$(FRONTEND_MAKE) coverage

build: ## Build the production backend binary and frontend assets natively.
	$(BACKEND_MAKE) build
	$(FRONTEND_MAKE) build

verify-fast: bootstrap generate lint architecture test coverage build ## Run all checks that do not require Docker.

verify: verify-fast e2e ## Run the complete verification cycle.

e2e: ## Run backend and browser E2E tests in an isolated Docker Compose project.
	DOCKER="$(DOCKER)" bash scripts/run-e2e

dev-up: ## Start the local application, PostgreSQL, and S3Mock.
	$(COMPOSE) up --detach --build postgres s3mock app

dev-down: ## Stop the local application without deleting its PostgreSQL volume.
	$(COMPOSE) down --remove-orphans

doctor: ## Print local toolchain versions and Docker availability.
	@go_version="$$(go version)"; \
	node_version="$$(node --version)"; \
	pnpm_version="$$(cd frontend && $(PNPM) --version)"; \
	docker_version="$$("$(DOCKER)" --version)"; \
	compose_version="$$("$(DOCKER)" compose version)"; \
	printf 'tools[5]{name,version}:\n'; \
	printf '  go,"%s"\n' "$$go_version"; \
	printf '  node,"%s"\n' "$$node_version"; \
	printf '  pnpm,"%s"\n' "$$pnpm_version"; \
	printf '  docker,"%s"\n' "$$docker_version"; \
	printf '  compose,"%s"\n' "$$compose_version"

logs: ## Follow the last 200 structured log events from the local stack.
	$(COMPOSE) logs --follow --tail=200

logs-service: ## Follow one service. Usage: make logs-service SERVICE=app
	@case "$${SERVICE:-}" in \
		""|*[!a-zA-Z0-9_-]*|[-_]*) \
			printf 'bin: "make logs-service"\nerror:\n  code: usage_error\n  message: "SERVICE must be a Compose service name"\nhelp: "Run make logs-service SERVICE=app"\n'; \
			exit 2; \
		;; \
	esac
	$(COMPOSE) logs --follow --tail=200 "$${SERVICE}"

db-shell: ## Open psql in the isolated local PostgreSQL container.
	$(COMPOSE) exec postgres psql --username analytics --dbname analytics

db-query: ## Run a local SQL query. Usage: make db-query QUERY='select now()'
	@test -n "$${QUERY:-}" || { \
		printf 'bin: "make db-query"\nerror:\n  code: usage_error\n  message: "QUERY is required"\nhelp: "Run make db-query with a QUERY value"\n'; \
		exit 2; \
	}
	$(COMPOSE) exec --no-TTY postgres psql --no-psqlrc --csv --quiet --username analytics --dbname analytics --set ON_ERROR_STOP=1 --command "$${QUERY}"

s3-ls: ## List objects in the isolated local S3 bucket.
	$(COMPOSE) --profile tools run --rm aws s3 ls s3://analytics --recursive

s3-get: ## Print one local object. Usage: make s3-get KEY=path/to/object
	@test -n "$${KEY:-}" || { \
		printf 'bin: "make s3-get"\nerror:\n  code: usage_error\n  message: "KEY is required"\nhelp: "Run make s3-get KEY=path/to/object"\n'; \
		exit 2; \
	}
	$(COMPOSE) --profile tools run --rm aws s3 cp "s3://analytics/$${KEY}" -

s3-put: ## Upload a workspace file locally. Usage: make s3-put FILE=tmp/a KEY=a
	@test -n "$${FILE:-}" && test -n "$${KEY:-}" || { \
		printf 'bin: "make s3-put"\nerror:\n  code: usage_error\n  message: "FILE and KEY are required"\nhelp: "Run make s3-put FILE=tmp/a KEY=a"\n'; \
		exit 2; \
	}
	@case "$${FILE}" in \
		/*|..|../*|*/..|*/../*) \
			printf 'bin: "make s3-put"\nerror:\n  code: usage_error\n  message: "FILE must stay inside the workspace"\nhelp: "Pass a repository-relative file path"\n'; \
			exit 2; \
		;; \
	esac
	@test -f "$${FILE}" || { \
		printf 'bin: "make s3-put"\nerror:\n  code: usage_error\n  message: "FILE must reference an existing regular file"\nhelp: "Pass a repository-relative file path"\n'; \
		exit 2; \
	}
	$(COMPOSE) --profile tools run --rm aws s3 cp "/workspace/$${FILE}" "s3://analytics/$${KEY}"

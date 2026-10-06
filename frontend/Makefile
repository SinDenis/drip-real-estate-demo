SHELL := /bin/bash
.DEFAULT_GOAL := help

PNPM := npx --yes pnpm@11.25.0

.PHONY: help bootstrap generate test lint architecture coverage build verify

help:
	@printf 'bin: "make -C frontend"\n'
	@printf 'description: "Frontend development and verification commands"\n'
	@printf 'commands[9]{name,description}:\n'
	@printf '  help,"Show this command contract"\n'
	@printf '  bootstrap,"Install pinned frontend dependencies"\n'
	@printf '  generate,"Regenerate the typed OpenAPI schema"\n'
	@printf '  test,"Run React and TypeScript tests"\n'
	@printf '  lint,"Run TypeScript lint and format checks"\n'
	@printf '  architecture,"Enforce frontend dependency boundaries"\n'
	@printf '  coverage,"Enforce all 90 percent coverage gates"\n'
	@printf '  build,"Type-check and build production assets"\n'
	@printf '  verify,"Run every native frontend check"\n'
	@printf 'help[1]: "Run make -C frontend verify before handoff"\n'

bootstrap:
	$(PNPM) install --frozen-lockfile

generate:
	$(PNPM) generate:api

test:
	$(PNPM) test

lint:
	$(PNPM) lint

architecture:
	$(PNPM) architecture

coverage:
	$(PNPM) coverage

build:
	$(PNPM) build

verify: bootstrap generate lint architecture test coverage build

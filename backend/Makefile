SHELL := /bin/bash
.DEFAULT_GOAL := help

.PHONY: help generate test lint architecture coverage build verify

help:
	@printf 'bin: "make -C backend"\n'
	@printf 'description: "Backend development and verification commands"\n'
	@printf 'commands[8]{name,description}:\n'
	@printf '  help,"Show this command contract"\n'
	@printf '  generate,"Regenerate Go code from OpenAPI"\n'
	@printf '  test,"Run Go tests with the race detector"\n'
	@printf '  lint,"Run Go linters and format checks"\n'
	@printf '  architecture,"Enforce backend dependency boundaries"\n'
	@printf '  coverage,"Enforce 90 percent statement coverage for the isolated Go core"\n'
	@printf '  build,"Build the backend API binary"\n'
	@printf '  verify,"Run every native backend check"\n'
	@printf 'help[1]: "Run make -C backend verify before handoff"\n'

generate:
	go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml

test:
	go test -race ./...

lint:
	go tool golangci-lint run

architecture:
	go test ./architecture

coverage:
	bash ../scripts/check-go-coverage

build:
	mkdir -p .build
	go build -o .build/api ./cmd/api

verify: generate lint architecture test coverage build

# Orxest — build and development tasks.
#
# The default target builds a single binary that contains the API and the web
# interface (spec §59.20).

SHELL := /bin/bash
BIN_DIR := bin
BINARY := $(BIN_DIR)/orxest
WEB_DIR := web
GO ?= go

.DEFAULT_GOAL := build

## build: build the Go binary with the embedded web interface
build: web
	$(GO) build -o $(BINARY) ./cmd/orxest
	@echo "built $(BINARY)"

## build-go: build the Go binary without rebuilding the web interface
build-go:
	$(GO) build -o $(BINARY) ./cmd/orxest

## web: install dependencies and build the frontend into web/dist
web:
	cd $(WEB_DIR) && npm install --no-fund --no-audit && npm run build

## web-dev: run the Vite dev server against a locally running orxest
web-dev:
	cd $(WEB_DIR) && npm run dev

## run: run the server on the default port
run: build-go
	./$(BINARY) serve

## test: run the complete test suite
test:
	$(GO) test -count=1 ./...

## test-race: run the test suite with the race detector
test-race:
	$(GO) test -race -count=1 ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## fmt: format the Go sources
fmt:
	gofmt -w cmd internal migrations

## tidy: tidy the module
tidy:
	$(GO) mod tidy

## migrate: apply database migrations to the configured database
migrate:
	$(GO) run ./cmd/orxest migrate

## openapi: regenerate the checked-in OpenAPI document
openapi:
	$(GO) run ./cmd/orxest openapi --out docs/openapi.json
	@echo "wrote docs/openapi.json"

## clean: remove build output
clean:
	rm -rf $(BIN_DIR) $(WEB_DIR)/dist/assets
	@echo "cleaned"

## help: list the available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build build-go web web-dev run test test-race vet fmt tidy migrate openapi clean help

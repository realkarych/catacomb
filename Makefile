SHELL := /bin/bash

APP_NAME := catacomb
DIST_DIR := bin
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

GOLANGCI_LINT_VERSION := v2.12.2
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
FUZZTIME ?= 20s

.PHONY: all build test cover lint fmt tidy clean help fuzz

all: help

## Build the catacomb binary into bin/
build:
	@mkdir -p $(DIST_DIR)
	@go build -ldflags="-s -w -X 'main.Version=$(VERSION)'" -o "$(DIST_DIR)/$(APP_NAME)" ./cmd/catacomb

## Run all tests with the race detector and a coverage profile
test:
	@go test -race -coverpkg=./... -coverprofile=coverage.out ./...

## Run tests then enforce the coverage threshold (.testcoverage.yml)
cover: test
	@go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml

## Fuzz every Fuzz* target for a short burst each (FUZZTIME, default 20s; not part of cover)
fuzz:
	@set -euo pipefail; \
	for pkg in $$(go list ./...); do \
		listed=$$(go test -list '^Fuzz' "$$pkg"); \
		for target in $$(grep '^Fuzz' <<<"$$listed" || true); do \
			echo "==> $$target ($$pkg)"; \
			go test -run '^$$' -fuzz "^$$target\$$" -fuzztime $(FUZZTIME) "$$pkg"; \
		done; \
	done

## Run linters (pinned golangci-lint)
lint:
	@$(GOLANGCI_LINT) run --timeout=5m ./...

## Apply formatters and import ordering (pinned golangci-lint)
fmt:
	@$(GOLANGCI_LINT) fmt ./...

## Tidy go.mod / go.sum
tidy:
	@go mod tidy

## Remove build and coverage artifacts
clean:
	@rm -rf $(DIST_DIR) coverage.out coverage.html

## Show available targets
help:
	@echo "Catacomb make targets:"
	@echo "  build   - build the binary into bin/"
	@echo "  test    - run tests with -race and a coverage profile"
	@echo "  cover   - test + enforce the 100% coverage gate"
	@echo "  fuzz    - fuzz every Fuzz* target for FUZZTIME (default 20s) each; not in cover"
	@echo "  lint    - run golangci-lint ($(GOLANGCI_LINT_VERSION), pinned)"
	@echo "  fmt     - apply gofumpt + goimports via golangci-lint ($(GOLANGCI_LINT_VERSION), pinned)"
	@echo "  tidy    - go mod tidy"
	@echo "  clean   - remove build and coverage artifacts"

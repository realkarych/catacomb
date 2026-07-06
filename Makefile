SHELL := /bin/bash

APP_NAME := catacomb
DIST_DIR := bin
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build test cover lint fmt tidy clean proto help fuzz

## Regenerate protobuf bindings (installs plugins to GOBIN, then runs buf generate)
proto:
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
	@buf generate

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

## Fuzz the reducer commutativity property for a short burst (not part of cover/CI)
fuzz:
	@go test -run '^$$' -fuzz '^FuzzReductionCommutativity$$' -fuzztime 30s ./reduce

## Run linters (golangci-lint)
lint:
	@golangci-lint run --timeout=5m ./...

## Apply formatters and import ordering (golangci-lint)
fmt:
	@golangci-lint fmt ./...

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
	@echo "  fuzz    - fuzz the reducer commutativity property (30s; not in cover/CI)"
	@echo "  lint    - run golangci-lint"
	@echo "  fmt     - apply gofumpt + goimports via golangci-lint"
	@echo "  tidy    - go mod tidy"
	@echo "  clean   - remove build and coverage artifacts"

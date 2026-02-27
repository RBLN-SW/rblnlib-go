include versions.mk

GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: help test test-device test-race tidy vendor fmt lint lint-fix

help:
	@echo "Available targets:"
	@echo "  make test         - run all package tests"
	@echo "  make fmt          - format all Go files"
	@echo "  make lint         - run golangci-lint"
	@echo "  make lint-fix     - run golangci-lint with auto-fixes"
	@echo "  make tidy         - run go mod tidy"
	@echo "  make vendor       - sync vendor directory"

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

lint:
	$(GOLANGCI_LINT) run

lint-fix:
	$(GOLANGCI_LINT) run --fix

tidy:
	$(GO) mod tidy

vendor:
	$(GO) mod vendor

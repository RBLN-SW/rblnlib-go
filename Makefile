include versions.mk

GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: help test test-device test-race vet check-linux tidy vendor fmt lint lint-fix

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

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

check-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build ./...
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build ./...

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

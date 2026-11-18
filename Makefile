include versions.mk

GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: help test test-device test-race vet check-linux generate-proto tidy vendor fmt lint lint-fix

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

generate-proto:
	mkdir -p bin/proto-tools
	GOBIN=$(CURDIR)/bin/proto-tools $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	GOBIN=$(CURDIR)/bin/proto-tools $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
	PATH=$(CURDIR)/bin/proto-tools:$$PATH protoc -I api --go_out=internal/proto/smdv1 --go_opt=paths=source_relative --go-grpc_out=internal/proto/smdv1 --go-grpc_opt=paths=source_relative api/rbln_smd_v1.proto
	PATH=$(CURDIR)/bin/proto-tools:$$PATH protoc -I api --go_out=internal/proto/smdlegacy --go_opt=paths=source_relative --go-grpc_out=internal/proto/smdlegacy --go-grpc_opt=paths=source_relative api/rbln_services.proto

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

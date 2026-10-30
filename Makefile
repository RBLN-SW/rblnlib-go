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

.PHONY: license-check
license-check: ## Check that tracked Go, shell and Python sources carry a license header.
	@fail=0; for f in $$(git ls-files '*.go' '*.sh' '*.py' | grep -v '^vendor/'); do \
		head -25 "$$f" | grep -q 'Copyright' || { echo "missing license header: $$f"; fail=1; }; \
	done; \
	[ $$fail -eq 0 ] && echo "license-check: ok" || exit 1

.PHONY: third-party-notices
third-party-notices: ## Regenerate THIRD_PARTY_NOTICES from the module dependency graph.
	python3 hack/gen-third-party-notices.py .

.PHONY: verify-third-party-notices
verify-third-party-notices: ## Check that THIRD_PARTY_NOTICES matches the dependency graph.
	@tmp=$$(mktemp); trap 'rm -f "$$tmp"' EXIT; \
	python3 hack/gen-third-party-notices.py . "$$tmp" >/dev/null || { echo "verify-third-party-notices: generator failed"; exit 1; }; \
	cmp -s "$$tmp" THIRD_PARTY_NOTICES || { echo "THIRD_PARTY_NOTICES is out of date; run 'make third-party-notices'"; exit 1; }; \
	echo "verify-third-party-notices: ok"

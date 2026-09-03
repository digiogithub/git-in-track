# Makefile — git-in-track
# The single local entry point. CI runs the same targets, so "works on my
# machine" and "works in CI" cannot drift apart.

SHELL          := /bin/bash
BINARY         := gintrack
CMD            := ./cmd/gintrack
BIN_DIR        := bin
WASM_OUT       := web/public/core.wasm
WASM_EXEC      := web/public/wasm_exec.js
WEB_DIR        := web
NODE           ?= node
PYTHON         ?= python3

VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT         ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE           ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS        := -s -w \
                  -X main.version=$(VERSION) \
                  -X main.commit=$(COMMIT) \
                  -X main.date=$(DATE) \
                  -X main.builtBy=make

# Package list without web/node_modules, where npm may install third-party Go
# sources that are not part of this module's source tree.
GO_PKGS         = $(shell go list ./... | grep -v '/web/node_modules/')

export CGO_ENABLED := 0

.DEFAULT_GOAL := build
.PHONY: help deps web wasm wasm-smoke build test test-go test-web lint lint-go lint-web lint-ci fmt run dev release-check release-snapshot clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

deps: ## Download Go modules and install npm dependencies
	go mod download
	cd $(WEB_DIR) && npm ci

wasm: ## Compile the shared Go core to WebAssembly
	GOOS=js GOARCH=wasm go build -trimpath -ldflags "-s -w" -o $(WASM_OUT) ./wasm
	@# The runtime shim must come from the toolchain that produced core.wasm:
	@# Go 1.24+ ships it under lib/wasm, earlier releases under misc/wasm.
	@if [ -f "$$(go env GOROOT)/lib/wasm/wasm_exec.js" ]; then \
	  cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" $(WASM_EXEC); \
	elif [ -f "$$(go env GOROOT)/misc/wasm/wasm_exec.js" ]; then \
	  cp "$$(go env GOROOT)/misc/wasm/wasm_exec.js" $(WASM_EXEC); \
	else \
	  echo "wasm_exec.js not found in $$(go env GOROOT)" >&2; exit 1; \
	fi
	@echo "wasm: $(WASM_OUT) ($$(go version | awk '{print $$3}')) + $(WASM_EXEC)"

wasm-smoke: wasm ## Instantiate core.wasm in Node and call into the Go core
	$(NODE) scripts/wasm-smoke.mjs

web: wasm ## Build the React app into web/dist
	cd $(WEB_DIR) && npm ci && npm run build

build: ## Build the gintrack binary, embedding web/dist
	mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

test: test-go test-web ## Run every test suite

# The race detector needs cgo, which the rest of the build deliberately disables.
test-go: export CGO_ENABLED := 1
test-go: ## Go tests with the race detector and coverage
	go test -race -covermode=atomic -coverprofile=coverage.out $(GO_PKGS)

test-web: ## Vitest unit tests
	cd $(WEB_DIR) && npm run test -- --run

lint: lint-go lint-web lint-ci ## Run every linter

lint-go: ## gofmt check, go vet and golangci-lint when installed
	@unformatted=$$(gofmt -l . | grep -v '^web/node_modules/' || true); \
	if [ -n "$$unformatted" ]; then \
	  echo "These files are not gofmt'ed:"; echo "$$unformatted"; exit 1; \
	fi
	go vet $(GO_PKGS)
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  golangci-lint run --timeout=5m; \
	else \
	  echo "golangci-lint not installed; skipping (see docs/10-development-guidelines.md)"; \
	fi

lint-web: ## ESLint and the TypeScript type check
	cd $(WEB_DIR) && npm run lint && npm run typecheck

lint-ci: ## Validate the GitHub Actions workflows
	@for f in .github/workflows/*.yml; do \
	  $(PYTHON) -c "import sys, yaml; yaml.safe_load(open(sys.argv[1]))" "$$f" || exit 1; \
	  echo "yaml ok: $$f"; \
	done
	@if command -v actionlint >/dev/null 2>&1; then \
	  actionlint .github/workflows/*.yml; \
	else \
	  echo "actionlint not installed; skipping (go install github.com/rhysd/actionlint/cmd/actionlint@latest)"; \
	fi

fmt: ## Format the Go sources
	gofmt -w $$(git ls-files '*.go')

run: build ## Build and start the companion server on 127.0.0.1:7317
	$(BIN_DIR)/$(BINARY) serve

dev: ## Vite dev server (run `make run` in another shell)
	cd $(WEB_DIR) && npm run dev

release-check: ## Validate .goreleaser.yaml without building anything
	go run github.com/goreleaser/goreleaser/v2@latest check

release-snapshot: ## Local GoReleaser dry run, no publishing (its before.hooks rebuild wasm + web)
	@if command -v goreleaser >/dev/null 2>&1; then \
	  goreleaser release --snapshot --clean --skip=publish; \
	else \
	  echo "goreleaser not installed: https://goreleaser.com/install/" >&2; exit 1; \
	fi

clean: ## Remove build outputs
	rm -rf $(BIN_DIR) dist $(WASM_OUT) $(WASM_EXEC) coverage.out
	@# web/dist itself must survive: `//go:embed all:dist` in web/embed.go needs
	@# the directory and its .gitkeep to exist for the Go build to compile.
	find $(WEB_DIR)/dist -mindepth 1 ! -name .gitkeep -delete

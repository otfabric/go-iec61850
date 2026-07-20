SHELL := /bin/bash

GO       ?= go
PKGS     := ./...
FUZZTIME ?= 15s

DIST     := dist

CMDS     := sclgen sclparse
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

PREFIX   ?= /usr/local

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
TAG        ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "")
COMMIT     ?= $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.tag=$(TAG) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: help test test-race test-verbose vet lint fmt fuzz bench tidy check clean coverage coverage-html coverage-clean interop scl-generate scl-check-generate build build-all release-all install ai-print-all ai-print-test ai-print ai-diff ai-digest ai-context

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	$(GO) test $(PKGS)

interop: ## Run interoperability tests against mms-interop adapter images (-tags=interop).
	$(GO) test -tags=interop -v -timeout 300s ./interop/...

test-verbose: ## Run unit tests with verbose output
	$(GO) test -v $(PKGS)

test-race: ## Run tests with race detector
	$(GO) test -race $(PKGS)

vet: ## Run go vet
	$(GO) vet $(PKGS)

lint: ## Run staticcheck
	@echo "Running staticcheck"
	@staticcheck $(PKGS)

lint-ci: ## Run golangci-lint
	@echo "Running golangci-lint"
	@golangci-lint run $(PKGS)

fmt: ## Format all Go source files
	@echo "Running gofmt"
	@gofmt -w .

coverage: ## Run tests with coverage profile and text summary
	$(GO) test -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -func=coverage.out | tee coverage.txt

coverage-html: coverage ## Generate HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html

coverage-clean: ## Remove coverage artifacts
	rm -f coverage.out coverage.txt coverage.html

fuzz: ## Run all fuzz targets for FUZZTIME each (default 15s)
	@echo "No fuzz targets defined yet (planned for M6)."

bench: ## Run all benchmarks
	$(GO) test -run=^$$ -bench=. -benchmem $(PKGS)

tidy: ## Tidy and verify module files
	$(GO) mod tidy
	$(GO) mod verify

scl-generate: ## Generate SCL raw types from XSD schemas
	$(GO) run ./scl/cmd/sclgen generate --spec-root ./scl/specs --out ./scl/internal/raw

scl-check-generate: ## Verify generated SCL code is up to date
	$(GO) run ./scl/cmd/sclgen check --spec-root ./scl/specs --out ./scl/internal/raw

build: ## Build all commands for the current platform
	@mkdir -p bin
	@for cmd in $(CMDS); do \
		echo "  BUILD $$cmd $(VERSION) -> bin/$$cmd"; \
		$(GO) build -ldflags '$(LDFLAGS)' -o bin/$$cmd ./scl/cmd/$$cmd || exit 1; \
	done

build-all: ## Cross-compile all commands for all platforms
	@mkdir -p $(DIST)
	@for cmd in $(CMDS); do \
		for target in $(PLATFORMS); do \
			os=$${target%/*}; arch=$${target#*/}; \
			ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
			out=$(DIST)/$$cmd-$$os-$$arch$$ext; \
			echo "  BUILD $$cmd $(VERSION) $$target -> $$out"; \
			GOOS=$$os GOARCH=$$arch $(GO) build -ldflags '$(LDFLAGS)' -o $$out ./scl/cmd/$$cmd || exit 1; \
		done; \
	done
	@echo "Built $$(ls $(DIST)/ | wc -l | tr -d ' ') binaries in $(DIST)/"

release-all: build-all ## Package release archives (tar.gz + zip) for all commands
	@for cmd in $(CMDS); do \
		for target in $(PLATFORMS); do \
			os=$${target%/*}; arch=$${target#*/}; \
			ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
			bin=$$cmd-$$os-$$arch$$ext; \
			name=$$cmd-$(VERSION)-$$os-$$arch; \
			if [ "$$os" = "windows" ]; then \
				(cd $(DIST) && cp $$bin $$cmd.exe && zip -q $$name.zip $$cmd.exe && rm $$cmd.exe); \
				echo "  ZIP  $(DIST)/$$name.zip"; \
			else \
				tar -czf $(DIST)/$$name.tar.gz -C $(DIST) $$bin; \
				echo "  TAR  $(DIST)/$$name.tar.gz"; \
			fi; \
		done; \
	done

install: build ## Install commands to PREFIX/bin (default /usr/local/bin)
	@for cmd in $(CMDS); do \
		sudo install -m 755 bin/$$cmd $(PREFIX)/bin/$$cmd; \
		echo "  INSTALL $(PREFIX)/bin/$$cmd"; \
	done

check: fmt tidy vet lint lint-ci test test-race coverage ## Run all pre-commit checks

clean: coverage-clean ## Clean test cache, coverage, and dist artifacts
	$(GO) clean -testcache
	rm -rf bin $(DIST)

ai-print-all: ## Print all Go files (code + tests)
	@find . -type f -name '*.go' ! -path './sources/*' -print0 | sort -z | \
	while IFS= read -r -d '' f; do \
		echo "=== START $$f ==="; \
		cat "$$f"; \
		echo; \
		echo "=== END $$f ==="; \
	done

ai-print-test: ## Print all Go files (tests)
	@find . -type f -name '*_test.go' ! -path './sources/*' -print0 | sort -z | \
	while IFS= read -r -d '' f; do \
		echo "=== START $$f ==="; \
		cat "$$f"; \
		echo; \
		echo "=== END $$f ==="; \
	done

ai-print: ## Print all Go files (code only no tests)
	@find . -type f -name '*.go' ! -name '*_test.go' ! -path './sources/*' -print0 | sort -z | \
	while IFS= read -r -d '' f; do \
		echo "=== START $$f ==="; \
		cat "$$f"; \
		echo; \
		echo "=== END $$f ==="; \
	done

ai-diff: ## Diff against main, including untracked files, plus full changed Go file dump
	@mkdir -p ai
	@files=$$( \
		{ \
			git diff --name-only main; \
			git ls-files --others --exclude-standard; \
		} | grep -E '\.go$$' | grep -v '^ai/' | sort -u \
	); \
	{ \
		git diff --binary main; \
		for f in $$(git ls-files --others --exclude-standard | grep -v '^ai/'); do \
			if [ -f "$$f" ]; then \
				echo; \
				echo "=== START NEW FILE: $$f ==="; \
				cat "$$f"; \
				echo; \
				echo "=== END NEW FILE: $$f ==="; \
			fi; \
		done; \
	} > ai/changes.patch; \
	{ \
		for f in $$files; do \
			if [ -f "$$f" ]; then \
				echo "=== START $$f ==="; \
				cat "$$f"; \
				echo; \
				echo "=== END $$f ==="; \
				echo; \
			fi; \
		done; \
	} > ai/changed.full
	@echo "Written ai/changes.patch"
	@echo "Written ai/changed.full"

ai-digest: ## Generate repo digest (structure only)
	@mkdir -p ai
	@echo "# Repo Digest" > ai/digest.md
	@echo "" >> ai/digest.md
	@echo "## Files" >> ai/digest.md
	@find . -type f -name '*.go' ! -path './sources/*' | sort | while read f; do \
		echo "- $$f" >> ai/digest.md; \
	done
	@echo "" >> ai/digest.md
	@echo "## Packages" >> ai/digest.md
	@go list ./... >> ai/digest.md 2>/dev/null || true
	@echo "Written ai/digest.md"

ai-context: ## Compress context
	@mkdir -p ai
	@echo "Summarize PROGRESS.md into max 30 lines." > ai/context.md
	@echo "Then append decisions and current state." >> ai/context.md
	@echo "" >> ai/context.md
	@cat PROGRESS.md >> ai/context.md
	@echo "Written ai/context.md"

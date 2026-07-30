SHELL := /bin/bash

# Parent otfabric/go.work must not override this module's go.mod pins.
export GOWORK := off

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

.PHONY: help test test-race test-verbose vet lint fmt fuzz bench tidy check clean coverage coverage-html coverage-clean interop scl-generate scl-check-generate build build-all release-all install ai-print-all ai-print-test ai-print ai-diff ai-digest ai-context vuln

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	@echo "Running unit tests"
	@go test $(PKGS)

interop: ## Run interoperability tests against mms-interop adapter images (-tags=interop).
	@echo "Running interoperability tests"
	@go test -tags=interop -v -timeout 300s ./interop/...

test-verbose: ## Run unit tests with verbose output
	@echo "Running tests with verbose output"
	@go test -v $(PKGS)

test-race: ## Run tests with race detector
	@echo "Running tests with race detector"
	@go test -race $(PKGS)

vet: ## Run go vet
	@echo "Running go vet"
	@go vet $(PKGS)

lint: ## Run staticcheck
	@echo "Running staticcheck"
	@staticcheck $(PKGS)

lint-ci: ## Run golangci-lint
	@echo "Running golangci-lint"
	@golangci-lint run $(PKGS)

vuln: ## Run govulncheck
	@echo "Running govulncheck"
	@govulncheck $(PKGS)

fmt: ## Format all Go source files
	@echo "Running gofmt"
	@gofmt -w .
	@echo "Running go fmt"
	@go fmt $(PKGS)

coverage: ## Run tests with coverage profile and text summary
	@echo "Running tests with coverage profile and text summary"
	@go test -coverprofile=coverage.out $(PKGS)
	@go tool cover -func=coverage.out | tee coverage.txt

coverage-html: coverage ## Generate HTML coverage report
	@echo "Generating HTML coverage report"
	@go tool cover -html=coverage.out -o coverage.html

coverage-clean: ## Remove coverage artifacts
	@echo "Removing coverage artifacts"
	rm -f coverage.out coverage.txt coverage.html

fuzz: ## Run all fuzz targets for FUZZTIME each (default 15s)
	@echo "=== Fuzzing iec61850 ==="
	@go test -fuzz=FuzzParseRef                   -fuzztime=$(FUZZTIME) .
	@go test -fuzz=FuzzParseFC                    -fuzztime=$(FUZZTIME) .
	@go test -fuzz=FuzzDecodeQuality              -fuzztime=$(FUZZTIME) .
	@go test -fuzz=FuzzNewValue                   -fuzztime=$(FUZZTIME) .
	@go test -fuzz=FuzzDecodeReportIndication     -fuzztime=$(FUZZTIME) .
	@go test -fuzz=FuzzDecodeOptFlds              -fuzztime=$(FUZZTIME) .
	@go test -fuzz=FuzzDecodeTrgOps               -fuzztime=$(FUZZTIME) .
	@go test -fuzz=FuzzDecodeReportIndication_NilValues -fuzztime=$(FUZZTIME) .
	@echo "=== Fuzzing scl ==="
	@go test -fuzz=FuzzParse                      -fuzztime=$(FUZZTIME) ./scl

bench: ## Run all benchmarks
	@echo "Running benchmarks"
	@go test -run=^$$ -bench=. -benchmem $(PKGS)

tidy: ## Tidy and verify module files
	@echo "Running go mod tidy"
	@go mod tidy
	@echo "Running go mod verify"
	@go mod verify

scl-generate: ## Generate SCL raw types from XSD schemas
	@echo "Generating SCL raw types from XSD schemas"
	@go run ./scl/cmd/sclgen generate --spec-root ./scl/specs --out ./scl/internal/raw

scl-check-generate: ## Verify generated SCL code is up to date
	@echo "Verifying generated SCL code is up to date"
	@go run ./scl/cmd/sclgen check --spec-root ./scl/specs --out ./scl/internal/raw

build: ## Build all commands for the current platform
	@echo "Building all commands for the current platform"
	@mkdir -p bin
	@for cmd in $(CMDS); do \
		echo "  BUILD $$cmd $(VERSION) -> bin/$$cmd"; \
		go build -ldflags '$(LDFLAGS)' -o bin/$$cmd ./scl/cmd/$$cmd || exit 1; \
	done

build-all: ## Cross-compile all commands for all platforms
	@echo "Cross-compiling all commands for all platforms"
	@mkdir -p $(DIST)
	@for cmd in $(CMDS); do \
		for target in $(PLATFORMS); do \
			os=$${target%/*}; arch=$${target#*/}; \
			ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
			out=$(DIST)/$$cmd-$$os-$$arch$$ext; \
			echo "  BUILD $$cmd $(VERSION) $$target -> $$out"; \
			GOOS=$$os GOARCH=$$arch @go build -ldflags '$(LDFLAGS)' -o $$out ./scl/cmd/$$cmd || exit 1; \
		done; \
	done
	@echo "Built $$(ls $(DIST)/ | wc -l | tr -d ' ') binaries in $(DIST)/"

release-all: build-all ## Package release archives (tar.gz + zip) for all commands
	@echo "Packaging release archives (tar.gz + zip) for all commands"
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
	@echo "Installing commands to PREFIX/bin (default /usr/local/bin)"
	@for cmd in $(CMDS); do \
		sudo install -m 755 bin/$$cmd $(PREFIX)/bin/$$cmd; \
		echo "  INSTALL $(PREFIX)/bin/$$cmd"; \
	done

check: fmt tidy vet lint lint-ci vuln test test-race coverage ## Run all pre-commit checks

clean: coverage-clean ## Clean test cache, coverage, and dist artifacts
	@echo "Cleaning test cache, coverage, and dist artifacts"
	@go clean -testcache
	rm -rf bin $(DIST)

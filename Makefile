SHELL := /bin/bash

# Parent otfabric/go.work must not override this module's go.mod pins.
export GOWORK := off

PKGS     := ./...
FUZZTIME ?= 15s

DIST     := release

CMDS     := sclgen sclparse
# The tools are nested modules (ADR-0009): ./... of this module does not reach them.
CLI_DIRS := $(CMDS:%=scl/cmd/%)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

PREFIX   ?= /usr/local

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
TAG        ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "")
COMMIT     ?= $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.tag=$(TAG) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: e2e interop-goose coverage-check coverage-pkg-check help test test-race test-verbose vet lint fmt fuzz bench tidy check clean coverage coverage-html coverage-clean interop scl-generate scl-check-generate build build-all release-all install ai-print-all ai-print-test ai-print ai-diff ai-digest ai-context vuln

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	@echo "Running unit tests"
	@go test $(PKGS)
	@for d in $(CLI_DIRS); do (cd $$d && go test ./...) || exit 1; done

e2e: ## Client against server on loopback TCP, and every example in-process (no Docker, no privileges)
	@echo "Running end-to-end tests"
	@go test -count=1 -race -run '^(TestClientReconnect|TestServerShutdown|TestListenAndServe|TestConcurrent|TestResourceExhaust)' .
	@go test -count=1 -race ./examples/...

interop: ## Run interoperability tests against iec61850-interop adapter images (-tags=interop).
	@echo "Running interoperability tests"
	@go test -tags=interop -v -timeout 300s ./interop/...

interop-goose: ## Run GOOSE and SV interop tests (Linux, root; needs the iec61850-interop-goose network, see interop/README.md)
	@go test -exec 'sudo -E' -tags=interop -v -timeout 600s -run 'GOOSE|SV_' ./interop/...

test-verbose: ## Run unit tests with verbose output
	@echo "Running tests with verbose output"
	@go test -v $(PKGS)

test-race: ## Run tests with race detector
	@echo "Running tests with race detector"
	@go test -race $(PKGS)
	@for d in $(CLI_DIRS); do (cd $$d && go test -race ./...) || exit 1; done

vet: ## Run go vet
	@echo "Running go vet"
	@go vet $(PKGS)
	@for d in $(CLI_DIRS); do (cd $$d && go vet ./...) || exit 1; done

lint: ## Run staticcheck
	@echo "Running staticcheck"
	@staticcheck $(PKGS)
	@for d in $(CLI_DIRS); do (cd $$d && staticcheck ./...) || exit 1; done

lint-ci: ## Run golangci-lint
	@echo "Running golangci-lint"
	@golangci-lint run $(PKGS)
	@for d in $(CLI_DIRS); do (cd $$d && golangci-lint run ./...) || exit 1; done

vuln: ## Run govulncheck
	@echo "Running govulncheck"
	@govulncheck $(PKGS)
	@for d in $(CLI_DIRS); do (cd $$d && govulncheck ./...) || exit 1; done

fmt: ## Format all Go source files
	@echo "Running gofmt"
	@gofmt -w .
	@echo "Running go fmt"
	@go fmt $(PKGS)

coverage: ## Run tests with coverage profile and text summary
	@echo "Running tests with coverage profile and text summary"
	@go test -coverprofile=coverage.out $(PKGS)
	@go tool cover -func=coverage.out | tee coverage.txt

# Same total as coverage-min in ci.yml; examples are not counted, as there.
COVER_MIN := 90

coverage-check: ## Fail when total coverage is below COVER_MIN
	@go test -covermode=atomic -coverprofile=coverage.out $$(go list $(PKGS) | grep -Ev '/examples?(/|$$)')
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% < %s%%\n", t, m; exit 1 } else printf "coverage %.1f%% >= %s%%\n", t, m }'

# Per-package floor (TEST-012). Lowest package when set: l2, 91.0 %. CI passes
# COVER_FLAGS="-exec sudo -race" so that the raw socket tests run.
COVER_PKG_MIN := 90
COVER_FLAGS ?=

coverage-pkg-check: ## Fail when a package is below COVER_PKG_MIN
	@go test $(COVER_FLAGS) -covermode=atomic -coverprofile=coverage.out $(PKGS)
	@go run scripts/covergate.go -min $(COVER_PKG_MIN) coverage.out

coverage-html: coverage ## Generate HTML coverage report
	@echo "Generating HTML coverage report"
	@go tool cover -html=coverage.out -o coverage.html

coverage-clean: ## Remove coverage artifacts
	@echo "Removing coverage artifacts"
	rm -f coverage.out coverage.txt coverage.html

fuzz: ## Run all fuzz targets for FUZZTIME each (default 15s)
	@echo "=== Fuzzing iec61850 ==="
	@go test -fuzz=^FuzzParseRef$$                   -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzParseFC$$                    -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzDecodeQuality$$              -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzNewValue$$                   -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzDecodeReportIndication$$     -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzDecodeOptFlds$$              -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzDecodeTrgOps$$               -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzDecodeReportIndication_NilValues$$ -fuzztime=$(FUZZTIME) .
	@echo "=== Fuzzing scl ==="
	@go test -fuzz=^FuzzParse$$                      -fuzztime=$(FUZZTIME) ./scl
	@go test -fuzz=^FuzzDecodeGoCB$$                 -fuzztime=$(FUZZTIME) .
	@go test -fuzz=^FuzzDecode$$                  -fuzztime=$(FUZZTIME) ./goose
	@go test -fuzz=^FuzzDecodeData$$                 -fuzztime=$(FUZZTIME) ./goose
	@go test -fuzz=^FuzzParseGSEAddress$$            -fuzztime=$(FUZZTIME) ./internal/servermodel

bench: ## Run all benchmarks
	@echo "Running benchmarks"
	@go test -run=^$$ -bench=. -benchmem $(PKGS)

tidy: ## Tidy and verify module files
	@echo "Running go mod tidy"
	@go mod tidy
	@for d in $(CLI_DIRS); do (cd $$d && go mod tidy) || exit 1; done
	@echo "Running go mod verify"
	@go mod verify
	@for d in $(CLI_DIRS); do (cd $$d && go mod verify) || exit 1; done

scl-generate: ## Generate SCL raw types from XSD schemas
	@echo "Generating SCL raw types from XSD schemas"
	@go -C scl/cmd/sclgen run . generate --spec-root $(CURDIR)/scl/specs --out $(CURDIR)/scl/internal/raw

scl-check-generate: ## Verify generated SCL code is up to date
	@echo "Verifying generated SCL code is up to date"
	@go -C scl/cmd/sclgen run . check --spec-root $(CURDIR)/scl/specs --out $(CURDIR)/scl/internal/raw

build: ## Build all commands for the current platform
	@echo "Building all commands for the current platform"
	@mkdir -p bin
	@for cmd in $(CMDS); do \
		echo "  BUILD $$cmd $(VERSION) -> bin/$$cmd"; \
		(cd scl/cmd/$$cmd && go build -ldflags '$(LDFLAGS)' -o $(CURDIR)/bin/$$cmd .) || exit 1; \
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
			(cd scl/cmd/$$cmd && GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' -o $(CURDIR)/$$out .) || exit 1; \
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

clean: coverage-clean ## Clean test cache, coverage, and release artifacts
	@echo "Cleaning test cache, coverage, and release artifacts"
	@go clean -testcache
	rm -rf bin $(DIST)

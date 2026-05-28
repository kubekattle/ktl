BINARY ?= torque
PKG ?= ./cmd/torque
BIN_DIR ?= bin
DIST_DIR ?= dist
GO ?= go
PERL ?= perl
GOTEST ?= $(GO) test
GOVET ?= $(GO) vet
HOST_GOOS := $(shell $(GO) env GOOS)
HOST_GOARCH := $(shell $(GO) env GOARCH)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GIT_TREE_STATE ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo clean || echo dirty)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS ?= -s -w \
	-X github.com/ingresslabs/torque/internal/version.Version=$(VERSION) \
	-X github.com/ingresslabs/torque/internal/version.GitCommit=$(GIT_COMMIT) \
	-X github.com/ingresslabs/torque/internal/version.GitTreeState=$(GIT_TREE_STATE) \
	-X github.com/ingresslabs/torque/internal/version.BuildDate=$(BUILD_DATE)
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
CROSS_PLATFORMS ?= $(RELEASE_PLATFORMS) windows/amd64
RELEASE_TOOLS ?= $(BINARY) torque-agent verifier verify torque-mcp
RELEASE_TOOL_ARTIFACTS := $(foreach platform,$(RELEASE_PLATFORMS),$(foreach tool,$(RELEASE_TOOLS),$(DIST_DIR)/$(tool)-$(subst /,-,$(platform))))
GH ?= gh
RELEASE_TAG ?= $(VERSION)
GH_RELEASE_TITLE ?= $(BINARY) $(RELEASE_TAG)
GH_RELEASE_NOTES ?= Automated release for $(RELEASE_TAG)
GH_RELEASE_NOTES_FILE ?=
GH_RELEASE_REPO ?=
GH_RELEASE_FLAGS ?=
GH_RELEASE_UPLOAD_FLAGS ?= --clobber
GH_RELEASE_PACKAGE_GLOBS ?= $(DIST_DIR)/*.deb $(DIST_DIR)/*.rpm
BUF_VERSION ?= v1.61.0
BUF ?= $(GO) run github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
PROTOC_GEN_GO_VERSION ?= v1.36.11
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.0
PROTO_BIN ?= $(shell $(GO) env GOPATH)/bin
GO_TEST_FLAGS ?= $(GOFLAGS)
REMOTE ?= origin
RELEASE_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
CHANGELOG_FILE ?= $(DIST_DIR)/CHANGELOG-$(RELEASE_TAG).md
PREVIOUS_TAG ?= $(shell git describe --tags --abbrev=0 HEAD~1 2>/dev/null)

VERIFY_BINARY ?= verify
VERIFY_PKG ?= ./cmd/verify
VERIFIER_BINARY ?= verifier
VERIFIER_PKG ?= ./cmd/verifier
HELMER_BINARY ?= helmer
HELMER_PKG ?= ./cmd/torque
HELMER_BUILD_MODE ?= helmer-only
HELMER_LDFLAGS ?= $(LDFLAGS) -X main.buildMode=$(HELMER_BUILD_MODE)
PACKAGECLI_BINARY ?= torque-package
PACKAGECLI_PKG ?= ./cmd/package
MCP_BINARY ?= torque-mcp
MCP_PKG ?= ./cmd/torque-mcp
AGENT_BINARY ?= torque-agent
AGENT_PKG ?= ./cmd/torque-agent
RELEASE_HELMER_ARTIFACTS := $(foreach platform,$(RELEASE_PLATFORMS),$(DIST_DIR)/$(HELMER_BINARY)-$(subst /,-,$(platform)))
RELEASE_PACKAGECLI_ARTIFACTS := $(foreach platform,$(RELEASE_PLATFORMS),$(DIST_DIR)/$(PACKAGECLI_BINARY)-$(subst /,-,$(platform)))
RELEASE_ARTIFACTS := $(RELEASE_TOOL_ARTIFACTS) $(RELEASE_HELMER_ARTIFACTS) $(RELEASE_PACKAGECLI_ARTIFACTS)
LOGS_BINARY ?= torque-logs
LOGS_PKG ?= ./cmd/torque
LOGS_BUILD_MODE ?= logs-only
LOGS_LDFLAGS ?= $(LDFLAGS) -X main.buildMode=$(LOGS_BUILD_MODE)

.DEFAULT_GOAL := help

.PHONY: help print-%
.PHONY: build build-% build-cross build-verifier build-verify build-helmer build-packagecli build-agent build-mcp build-logs build-all
.PHONY: install install-verifier install-verify install-helmer install-packagecli install-agent install-mcp install-all
.PHONY: release dist-checksums dist-checksums-all gh-release gh-release-all tag-release push-release changelog package
.PHONY: test test-short test-integration test-ci smoke-package-verify verify-charts-e2e
.PHONY: testpoint testpoint-ci testpoint-unit testpoint-integration testpoint-charts-e2e testpoint-e2e-real testpoint-all
.PHONY: fmt lint tidy verify preflight docs docs-no-gifs site site-check proto proto-lint clean loc
PACKAGE_IMAGE ?= torque-packager
PACKAGE_PLATFORMS ?= linux/amd64

help: ## Show this help menu
	@echo "Available targets:"
	@LC_ALL=C grep -hE '^[a-zA-Z0-9_.%-]+:.*##' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS=":.*## "} {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

build: ## Build torque for the current platform into ./bin/torque
	@echo ">> building $(BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

build-verify: ## Build verify for the current platform into ./bin/verify
	@echo ">> building $(VERIFY_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(VERIFY_BINARY) $(VERIFY_PKG)

build-verifier: ## Build verifier for the current platform into ./bin/verifier
	@echo ">> building $(VERIFIER_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(VERIFIER_BINARY) $(VERIFIER_PKG)

build-helmer: ## Build helmer for the current platform into ./bin/helmer
	@echo ">> building $(HELMER_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(HELMER_LDFLAGS)' -o $(BIN_DIR)/$(HELMER_BINARY) $(HELMER_PKG)

build-packagecli: ## Build chart archive CLI for the current platform into ./bin/torque-package
	@echo ">> building $(PACKAGECLI_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(PACKAGECLI_BINARY) $(PACKAGECLI_PKG)

build-agent: ## Build gRPC agent for the current platform into ./bin/torque-agent
	@echo ">> building $(AGENT_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(AGENT_BINARY) $(AGENT_PKG)

build-mcp: ## Build MCP server for the current platform into ./bin/torque-mcp
	@echo ">> building $(MCP_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(MCP_BINARY) $(MCP_PKG)

build-logs: ## Build logs-only torque CLI for the current platform into ./bin/torque-logs
	@echo ">> building $(LOGS_BINARY) (logs-only) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LOGS_LDFLAGS)' -o $(BIN_DIR)/$(LOGS_BINARY) $(LOGS_PKG)

build-cross: ## Build torque for configured cross platforms into ./bin
	@echo ">> building cross-platform binaries for: $(CROSS_PLATFORMS)"
	@set -e; for platform in $(CROSS_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		$(MAKE) build-$$os-$$arch; \
	done
	@echo ">> cross-platform build complete"

build-%: ## Build torque for <os>-<arch> into ./bin/torque-<os>-<arch>[.exe]
	@mkdir -p $(BIN_DIR)
	@target="$*"; os="$${target%-*}"; arch="$${target#*-}"; \
	if [ "$$os" = "$$arch" ]; then \
		printf "invalid build target '%s' (expected os-arch)\n" "$*"; \
		exit 1; \
	fi; \
	out="$(BIN_DIR)/$(BINARY)-$$os-$$arch"; \
	if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
	echo ">> building $(BINARY) for $$os/$$arch -> $$out"; \
	GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $$out $(PKG)

build-all: build build-agent build-verifier build-verify build-helmer build-packagecli build-mcp ## Build torque and standalone toolkit binaries

install: ## Install torque into GOPATH/bin or GOBIN
	@echo ">> installing $(BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(PKG)

install-verify: ## Install verify into GOPATH/bin or GOBIN
	@echo ">> installing $(VERIFY_BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(VERIFY_PKG)

install-verifier: ## Install verifier into GOPATH/bin or GOBIN
	@echo ">> installing $(VERIFIER_BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(VERIFIER_PKG)

install-helmer: ## Install helmer into GOPATH/bin or GOBIN
	@echo ">> installing $(HELMER_BINARY) ($(VERSION))"
	@dest="$$($(GO) env GOBIN)"; \
	if [ -z "$$dest" ]; then dest="$$($(GO) env GOPATH)/bin"; fi; \
	mkdir -p "$$dest"; \
	$(GO) build $(GOFLAGS) -ldflags '$(HELMER_LDFLAGS)' -o "$$dest/$(HELMER_BINARY)" $(HELMER_PKG)

install-packagecli: ## Install torque-package into GOPATH/bin or GOBIN
	@echo ">> installing $(PACKAGECLI_BINARY) ($(VERSION))"
	@dest="$$($(GO) env GOBIN)"; \
	if [ -z "$$dest" ]; then dest="$$($(GO) env GOPATH)/bin"; fi; \
	mkdir -p "$$dest"; \
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o "$$dest/$(PACKAGECLI_BINARY)" $(PACKAGECLI_PKG)

install-agent: ## Install torque-agent into GOPATH/bin or GOBIN
	@echo ">> installing $(AGENT_BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(AGENT_PKG)

install-mcp: ## Install torque-mcp into GOPATH/bin or GOBIN
	@echo ">> installing $(MCP_BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(MCP_PKG)

install-all: ## Install torque and standalone toolkit binaries
	$(MAKE) install
	$(MAKE) install-agent
	$(MAKE) install-verifier
	$(MAKE) install-verify
	$(MAKE) install-helmer
	$(MAKE) install-packagecli
	$(MAKE) install-mcp

release: ## Cross-build release artifacts into ./dist
	@echo ">> building release artifacts for: $(RELEASE_PLATFORMS)"
	@mkdir -p $(DIST_DIR)
	@set -e; for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		for tool in $(RELEASE_TOOLS); do \
			out="$(DIST_DIR)/$$tool-$$os-$$arch"; \
			if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
			echo "   - $$os/$$arch -> $$out"; \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/$$tool; \
		done; \
		out="$(DIST_DIR)/$(HELMER_BINARY)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
		echo "   - $$os/$$arch -> $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(HELMER_LDFLAGS)' -o $$out $(HELMER_PKG); \
		out="$(DIST_DIR)/$(PACKAGECLI_BINARY)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
		echo "   - $$os/$$arch -> $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS)' -o $$out $(PACKAGECLI_PKG); \
	done

dist-checksums: release ## Generate sha256 checksums for release artifacts in ./dist
	@mkdir -p "$(DIST_DIR)"
	@echo ">> generating checksums for release artifacts"
	@rm -f "$(DIST_DIR)"/*.sha256 "$(DIST_DIR)/checksums.txt"
	@sha_cmd=""; \
	if command -v sha256sum >/dev/null 2>&1; then sha_cmd="sha256sum"; \
	elif command -v shasum >/dev/null 2>&1; then sha_cmd="shasum -a 256"; \
	else echo "error: sha256sum or shasum not found in PATH" >&2; exit 1; fi; \
	cd "$(DIST_DIR)"; \
	for f in $(notdir $(RELEASE_ARTIFACTS)); do \
		if [ ! -f "$$f" ]; then echo "error: missing $(DIST_DIR)/$$f (expected from make release)" >&2; exit 1; fi; \
		$$sha_cmd "$$f" > "$$f.sha256"; \
	done; \
	cat *.sha256 > checksums.txt

dist-checksums-all: release package ## Generate sha256 checksums for release artifacts + .deb/.rpm in ./dist
	@mkdir -p "$(DIST_DIR)"
	@echo ">> generating checksums for release artifacts + packages"
	@rm -f "$(DIST_DIR)"/*.sha256 "$(DIST_DIR)/checksums.txt"
	@sha_cmd=""; \
	if command -v sha256sum >/dev/null 2>&1; then sha_cmd="sha256sum"; \
	elif command -v shasum >/dev/null 2>&1; then sha_cmd="shasum -a 256"; \
	else echo "error: sha256sum or shasum not found in PATH" >&2; exit 1; fi; \
	cd "$(DIST_DIR)"; \
	files=""; \
	for f in $(notdir $(RELEASE_ARTIFACTS)); do \
		if [ ! -f "$$f" ]; then echo "error: missing $(DIST_DIR)/$$f (expected from make release)" >&2; exit 1; fi; \
		files="$$files $$f"; \
	done; \
	for f in *.deb *.rpm; do \
		if [ -f "$$f" ]; then files="$$files $$f"; fi; \
	done; \
	for f in $$files; do \
		$$sha_cmd "$$f" > "$$f.sha256"; \
	done; \
	cat *.sha256 > checksums.txt

gh-release: dist-checksums ## Publish release artifacts to GitHub via gh CLI
	@if ! command -v $(GH) >/dev/null 2>&1; then \
		echo "error: GitHub CLI '$(GH)' not found in PATH"; \
		exit 1; \
	fi
	@notes_flag="--notes"; notes_value="$(GH_RELEASE_NOTES)"; \
	if [ -n "$(GH_RELEASE_NOTES_FILE)" ]; then \
		notes_flag="--notes-file"; \
		notes_value="$(GH_RELEASE_NOTES_FILE)"; \
	fi; \
	echo ">> creating GitHub release $(RELEASE_TAG)"; \
	repo_flag=""; \
	if [ -n "$(GH_RELEASE_REPO)" ]; then repo_flag="--repo $(GH_RELEASE_REPO)"; fi; \
	$(GH) release create $$repo_flag $(RELEASE_TAG) $(RELEASE_ARTIFACTS) $(DIST_DIR)/checksums.txt $(DIST_DIR)/*.sha256 --title "$(GH_RELEASE_TITLE)" $$notes_flag "$$notes_value" $(GH_RELEASE_FLAGS)

gh-release-all: dist-checksums-all ## Publish release artifacts + .deb/.rpm to GitHub via gh CLI
	@if ! command -v $(GH) >/dev/null 2>&1; then \
		echo "error: GitHub CLI '$(GH)' not found in PATH"; \
		exit 1; \
	fi
	@notes_flag="--notes"; notes_value="$(GH_RELEASE_NOTES)"; \
	if [ -n "$(GH_RELEASE_NOTES_FILE)" ]; then \
		notes_flag="--notes-file"; \
		notes_value="$(GH_RELEASE_NOTES_FILE)"; \
	fi; \
	repo_flag=""; \
	if [ -n "$(GH_RELEASE_REPO)" ]; then repo_flag="--repo $(GH_RELEASE_REPO)"; fi; \
	echo ">> preparing GitHub release assets for $(RELEASE_TAG)"; \
	set -- $(RELEASE_ARTIFACTS); \
	for f in $(GH_RELEASE_PACKAGE_GLOBS); do \
		if [ -f "$$f" ]; then set -- "$$@" "$$f"; fi; \
	done; \
	for f in $(DIST_DIR)/checksums.txt $(DIST_DIR)/*.sha256; do \
		if [ -f "$$f" ]; then set -- "$$@" "$$f"; fi; \
	done; \
	if $(GH) release view $$repo_flag "$(RELEASE_TAG)" >/dev/null 2>&1; then \
		echo ">> uploading assets to existing GitHub release $(RELEASE_TAG)"; \
		$(GH) release upload $$repo_flag "$(RELEASE_TAG)" "$$@" $(GH_RELEASE_UPLOAD_FLAGS); \
	else \
		echo ">> creating GitHub release $(RELEASE_TAG)"; \
		$(GH) release create $$repo_flag "$(RELEASE_TAG)" "$$@" --title "$(GH_RELEASE_TITLE)" $$notes_flag "$$notes_value" $(GH_RELEASE_FLAGS); \
	fi

tag-release: ## Create an annotated git tag for $(RELEASE_TAG)
	@if [ -z "$(RELEASE_TAG)" ]; then \
		echo "error: RELEASE_TAG is required (example: make tag-release RELEASE_TAG=v1.2.3)"; \
		exit 1; \
	fi
	@if ! git diff --quiet --ignore-submodules --; then \
		echo "error: working tree has uncommitted changes"; \
		exit 1; \
	fi
	@if ! git diff --cached --quiet --ignore-submodules --; then \
		echo "error: staged but uncommitted changes detected"; \
		exit 1; \
	fi
	@if git rev-parse -q --verify "refs/tags/$(RELEASE_TAG)" >/dev/null; then \
		echo "error: tag $(RELEASE_TAG) already exists"; \
		exit 1; \
	fi
	@echo ">> tagging $(RELEASE_TAG)"
	git tag -a "$(RELEASE_TAG)" -m "Release $(RELEASE_TAG)"

push-release: ## Push $(RELEASE_BRANCH) and $(RELEASE_TAG) to $(REMOTE)
	@branch="$(RELEASE_BRANCH)"; tag="$(RELEASE_TAG)"; \
	if [ -z "$$tag" ]; then \
		echo "error: RELEASE_TAG is required (example: make push-release RELEASE_TAG=v1.2.3)"; \
		exit 1; \
	fi; \
	if [ -z "$$branch" ]; then \
		echo "error: could not determine current branch; set RELEASE_BRANCH"; \
		exit 1; \
	fi; \
	if ! git rev-parse -q --verify "refs/tags/$$tag" >/dev/null; then \
		echo "error: tag $$tag does not exist; run make tag-release first"; \
		exit 1; \
	fi; \
	echo ">> pushing $$branch to $(REMOTE)"; \
	git push $(REMOTE) $$branch; \
	echo ">> pushing tag $$tag to $(REMOTE)"; \
	git push $(REMOTE) $$tag

changelog: ## Generate changelog from $(PREVIOUS_TAG) to HEAD into $(CHANGELOG_FILE)
	@if [ -z "$(RELEASE_TAG)" ]; then \
		echo "error: RELEASE_TAG is required (example: make changelog RELEASE_TAG=v1.2.3)"; \
		exit 1; \
	fi
	@mkdir -p $(DIST_DIR)
	@previous="$(PREVIOUS_TAG)"; next="$(RELEASE_TAG)"; out="$(CHANGELOG_FILE)"; \
	if [ -z "$$out" ] || [ "$$out" = "$(DIST_DIR)/CHANGELOG-.md" ]; then \
		out="$(DIST_DIR)/CHANGELOG-$$next.md"; \
	fi; \
	if [ -n "$$previous" ]; then \
		range="$$previous..HEAD"; \
		echo ">> generating changelog from $$previous to HEAD"; \
	else \
		range="HEAD"; \
		echo ">> generating changelog for entire history"; \
	fi; \
	echo ">> writing changelog to $$out"; \
	{ \
		echo "# $(BINARY) $$next"; \
		echo ""; \
		if [ -n "$$previous" ]; then \
			echo "Changes since $$previous:"; \
		else \
			echo "Changes:"; \
		fi; \
		echo ""; \
		git log $$range --pretty=format:'- %s (%h)' --no-merges; \
		echo ""; \
		echo "_Generated on $$(date -u '+%Y-%m-%dT%H:%M:%SZ')_"; \
	} > "$$out"; \
	echo "Set GH_RELEASE_NOTES_FILE=$$out to publish these notes"

test: ## Run Go tests across the repo
	$(GOTEST) $(GO_TEST_FLAGS) ./...

test-short: ## Run Go tests with -short
	$(GOTEST) $(GO_TEST_FLAGS) -short ./...

test-integration: ## Run integration tests (requires cluster access)
	$(GOTEST) $(GO_TEST_FLAGS) ./integration/...

smoke-package-verify: ## Package a sample chart and verify the archive (local smoke)
	@mkdir -p $(DIST_DIR)
	go run ./cmd/package --force --output $(DIST_DIR)/smoke-chart.sqlite ./testdata/charts/drift-guard
	go run ./cmd/package --verify $(DIST_DIR)/smoke-chart.sqlite

test-ci: ## Run fmt, lint, test, and package/verify smoke (CI parity)
	$(MAKE) fmt
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) smoke-package-verify

verify-charts-e2e: ## Run verify against allowlisted charts in testdata/charts
	./integration/verify_charts_e2e.sh

testpoint: ## Single entrypoint for fmt/lint/tests (scripts/testpoint.sh)
	./scripts/testpoint.sh

testpoint-ci: ## CI-flavored testpoint run (format check, go mod verify, unit tests + smoke)
	./scripts/testpoint.sh --ci --json-out /tmp/go-test.json

testpoint-unit: ## Unit testpoint run (fmt + lint + unit + smoke)
	./scripts/testpoint.sh

testpoint-integration: ## Integration testpoint run (adds tagged integration tests)
	./scripts/testpoint.sh --integration

testpoint-charts-e2e: ## Chart verify e2e (allowlist) via integration/verify_charts_e2e.sh
	./scripts/testpoint.sh --charts-e2e

testpoint-e2e-real: ## Real-cluster e2e (requires env; see scripts/testpoint.sh --help)
	./scripts/testpoint.sh --e2e-real

testpoint-all: ## Full testpoint run (unit + integration + charts-e2e)
	./scripts/testpoint.sh --integration --charts-e2e

fmt: ## Format all Go files in the module
	@echo ">> go fmt ./..."
	@$(GO) fmt ./...

lint: ## Run go vet (and staticcheck when available)
	@echo ">> go vet ./..."
	@$(GOVET) ./...
	@if command -v staticcheck >/dev/null 2>&1; then \
		echo ">> staticcheck ./..."; \
		staticcheck ./...; \
	else \
		echo ">> staticcheck not installed; skipping"; \
	fi
	@./scripts/check-docs-no-gifs.sh

tidy: ## Ensure go.mod/go.sum are tidy
	$(GO) mod tidy

verify: ## Run fmt, lint, and test
	$(MAKE) fmt lint test

preflight: verify ## Alias for verify (fmt + lint + unit tests)

package: ## Build .deb/.rpm packages into ./dist (Docker-based)
	@mkdir -p "$(DIST_DIR)"
	@set -e; for platform in $(PACKAGE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		image="$(PACKAGE_IMAGE)-$$arch"; \
		echo ">> building packaging image $$image ($$platform)"; \
		docker buildx build --load --platform "$$platform" -f packaging/Dockerfile -t "$$image" .; \
		echo ">> packaging $$platform"; \
		docker run --rm --platform "$$platform" \
			-e VERSION="$(VERSION)" \
			-e LDFLAGS="$(LDFLAGS)" \
			-e TARGETOS="$$os" \
			-e TARGETARCH="$$arch" \
			-e OUT_DIR="/out" \
			-v "$$(pwd):/src" \
			-v "$$(pwd)/$(DIST_DIR):/out" \
			"$$image"; \
	done

docs: site docs-no-gifs ## Generate static docs/site output and validate docs surfaces

docs-no-gifs: ## Ensure docs and generated site output do not contain GIFs
	@./scripts/check-docs-no-gifs.sh

site: ## Generate the static Pages site under ./site (landing + docs + index.json)
	@./scripts/gen-site.sh

site-check: ## Verify that ./site is up to date (fails if regen would change files)
	@tmp="$$(mktemp -d)"; \
	OUT_DIR="$$tmp/site" ./scripts/gen-site.sh >/dev/null; \
	diff -ruN --exclude='.DS_Store' --exclude='.gitkeep' --exclude='*.log' ./site "$$tmp/site" >/dev/null; \
	rm -rf "$$tmp"; \
	echo ">> site: OK"

proto: ## Generate gRPC/protobuf stubs under pkg/api
	GOBIN=$(PROTO_BIN) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN=$(PROTO_BIN) $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	PATH="$(PROTO_BIN):$$PATH" $(BUF) generate
	# Normalize the descriptor escape before log_file for clean text searches.
	$(PERL) -0pi -e 's/"\\\x62\x6c\x6f\x67_file/"\\x08log_file/g' pkg/api/torque/api/v1/agent.pb.go

proto-lint: ## Lint protobuf definitions
	$(BUF) lint

clean: ## Remove build artifacts (bin/ and dist/)
	rm -rf $(BIN_DIR) $(DIST_DIR)

# ----- METRICS -----
loc: ## Count Go lines of code (excluding vendor/ and bin/)
	@echo ">> Counting Go LOC (excluding vendor/ and bin/)"
	@find . -type f -name '*.go' ! -path '*/vendor/*' ! -path "./$(BIN_DIR)/*" -exec cat {} + | wc -l

print-%: ## Print the value of any Makefile variable
	@printf '%s=%s\n' '$*' '$($*)'

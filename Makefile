BINARY ?= ktl
PKG ?= ./cmd/ktl
BIN_DIR ?= bin
DIST_DIR ?= dist
GO ?= go
GOTEST ?= $(GO) test
GOVET ?= $(GO) vet
HOST_GOOS := $(shell $(GO) env GOOS)
HOST_GOARCH := $(shell $(GO) env GOARCH)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GIT_TREE_STATE ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo clean || echo dirty)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS ?= -s -w \
	-X github.com/kubekattle/ktl/internal/version.Version=$(VERSION) \
	-X github.com/kubekattle/ktl/internal/version.GitCommit=$(GIT_COMMIT) \
	-X github.com/kubekattle/ktl/internal/version.GitTreeState=$(GIT_TREE_STATE) \
	-X github.com/kubekattle/ktl/internal/version.BuildDate=$(BUILD_DATE)
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
RELEASE_TOOLS ?= $(BINARY) verify package
RELEASE_ARTIFACTS := $(foreach platform,$(RELEASE_PLATFORMS),$(foreach tool,$(RELEASE_TOOLS),$(DIST_DIR)/$(tool)-$(subst /,-,$(platform))))
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
GO_TEST_FLAGS ?= $(GOFLAGS)
REMOTE ?= origin
RELEASE_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
CHANGELOG_FILE ?= $(DIST_DIR)/CHANGELOG-$(RELEASE_TAG).md
PREVIOUS_TAG ?= $(shell git describe --tags --abbrev=0 HEAD~1 2>/dev/null)

CAPTURE_BINARY ?= capture
CAPTURE_PKG ?= ./cmd/capture
VERIFY_BINARY ?= verify
VERIFY_PKG ?= ./cmd/verify
PACKAGECLI_BINARY ?= package
PACKAGECLI_PKG ?= ./cmd/package
LOGS_BINARY ?= logs
LOGS_PKG ?= ./cmd/ktl
LOGS_BUILD_MODE ?= logs-only
LOGS_LDFLAGS ?= $(LDFLAGS) -X github.com/kubekattle/ktl/cmd/ktl.buildMode=$(LOGS_BUILD_MODE)

.DEFAULT_GOAL := help

.PHONY: help build build-% build-capture build-verify build-packagecli build-logs build-all install install-capture install-verify install-packagecli install-all release dist-checksums dist-checksums-all gh-release gh-release-all tag-release push-release changelog test test-short test-integration fmt lint tidy verify preflight docs proto proto-lint clean loc print-% test-ci smoke-package-verify verify-charts-e2e testpoint testpoint-ci testpoint-unit testpoint-integration testpoint-charts-e2e testpoint-e2e-real testpoint-all
PACKAGE_IMAGE ?= ktl-packager
PACKAGE_PLATFORMS ?= linux/amd64

help: ## Show this help menu
	@echo "Available targets:"
	@LC_ALL=C grep -hE '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS=":.*## "} {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

build: ## Build ktl for the current platform into ./bin/ktl
	@echo ">> building $(BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

build-capture: ## Build capture for the current platform into ./bin/capture
	@echo ">> building $(CAPTURE_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(CAPTURE_BINARY) $(CAPTURE_PKG)

build-verify: ## Build verify for the current platform into ./bin/verify
	@echo ">> building $(VERIFY_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(VERIFY_BINARY) $(VERIFY_PKG)

build-packagecli: ## Build package CLI for the current platform into ./bin/package
	@echo ">> building $(PACKAGECLI_BINARY) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(PACKAGECLI_BINARY) $(PACKAGECLI_PKG)

build-logs: ## Build logs-only ktl CLI for the current platform into ./bin/ktl-logs
	@echo ">> building $(LOGS_BINARY) (logs-only) for $(HOST_GOOS)/$(HOST_GOARCH)"
	@mkdir -p $(BIN_DIR)
	GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(LOGS_LDFLAGS)' -o $(BIN_DIR)/$(LOGS_BINARY) $(LOGS_PKG)

build-cross: ## Build binaries for all supported platforms (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)
	@echo ">> building cross-platform binaries into $(BIN_DIR)"
	@mkdir -p $(BIN_DIR)
	@# Linux amd64
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY)-linux-amd64 $(PKG)
	@# Linux arm64
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY)-linux-arm64 $(PKG)
	@# Darwin amd64
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY)-darwin-amd64 $(PKG)
	@# Darwin arm64
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY)-darwin-arm64 $(PKG)
	@# Windows amd64
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY)-windows-amd64.exe $(PKG)
	@echo ">> cross-platform build complete"

build-%: ## Build ktl for <os>-<arch> into ./bin/ktl-<os>-<arch>[.exe]
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

install: ## Install ktl into GOPATH/bin or GOBIN
	@echo ">> installing $(BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(PKG)

install-capture: ## Install capture into GOPATH/bin or GOBIN
	@echo ">> installing $(CAPTURE_BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CAPTURE_PKG)

install-verify: ## Install verify into GOPATH/bin or GOBIN
	@echo ">> installing $(VERIFY_BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(VERIFY_PKG)

install-packagecli: ## Install package CLI into GOPATH/bin or GOBIN
	@echo ">> installing $(PACKAGECLI_BINARY) ($(VERSION))"
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(PACKAGECLI_PKG)

install-all: ## Install ktl, verify, and package
	$(MAKE) install
	$(MAKE) install-verify
	$(MAKE) install-packagecli

release: ## Cross-build release artifacts into ./dist
	@echo ">> building release artifacts for: $(RELEASE_PLATFORMS)"
	@mkdir -p $(DIST_DIR)
	@for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		out="$(DIST_DIR)/$(BINARY)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
		echo "   - $$os/$$arch -> $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS)' -o $$out $(PKG); \
		for tool in verify package; do \
			out2="$(DIST_DIR)/$$tool-$$os-$$arch"; \
			if [ "$$os" = "windows" ]; then out2="$$out2.exe"; fi; \
			echo "   - $$os/$$arch -> $$out2"; \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS)' -o $$out2 ./cmd/$$tool; \
		done; \
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
	go run ./cmd/package --output $(DIST_DIR)/smoke-chart.sqlite ./testdata/charts/drift-guard
	go run ./cmd/package --verify $(DIST_DIR)/smoke-chart.sqlite

test-ci: ## Run fmt, lint, test, and package/verify smoke (CI parity)
	$(MAKE) fmt
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) smoke-package-verify

verify-charts-e2e: ## Run verify across all charts in testdata/charts (requires verify binary)
	VERIFY_BIN="$(BIN_DIR)/verify" ./integration/verify_charts_e2e.sh

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
	@command -v staticcheck >/dev/null 2>&1 && { \
		echo ">> staticcheck ./..."; \
		staticcheck ./...; \
	} || echo ">> staticcheck not installed; skipping"

tidy: ## Ensure go.mod/go.sum are tidy
	$(GO) mod tidy

verify: ## Run fmt, lint, and test
	$(MAKE) fmt lint test

preflight: verify ## Alias for verify (fmt + lint + unit tests)

package: ## Build .deb/.rpm packages into ./dist (Docker-based)
	@mkdir -p "$(DIST_DIR)"
	@for platform in $(PACKAGE_PLATFORMS); do \
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

docs: ## (No-op) Docs build pipeline is not checked in
	@echo ">> docs: no docs build pipeline is checked into this repo"

proto: ## Generate gRPC/protobuf stubs under pkg/api
	$(BUF) generate

proto-lint: ## Lint protobuf definitions
	$(BUF) lint

clean: ## Remove build artifacts (bin/ and dist/)
	rm -rf $(BIN_DIR) $(DIST_DIR)

# ----- METRICS -----
loc: ## Count Go lines of code (excluding vendor/ and bin/)
	@echo ">> Counting Go LOC (excluding vendor/ and bin/)"
	@find . -type f -name '*.go' ! -path "./vendor/*" ! -path "./$(BIN_DIR)/*" -exec cat {} + | wc -l

print-%: ## Print the value of any Makefile variable
	@printf '%s=%s\n' '$*' '$($*)'

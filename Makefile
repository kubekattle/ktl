BINARY ?= ktl
PKG ?= ./cmd/ktl
BIN_DIR ?= bin
DIST_DIR ?= dist
GO ?= go
GOTEST ?= $(GO) test
GOVET ?= $(GO) vet
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GIT_TREE_STATE ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo clean || echo dirty)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS ?= -s -w \
	-X github.com/example/ktl/internal/version.Version=$(VERSION) \
	-X github.com/example/ktl/internal/version.GitCommit=$(GIT_COMMIT) \
	-X github.com/example/ktl/internal/version.GitTreeState=$(GIT_TREE_STATE) \
	-X github.com/example/ktl/internal/version.BuildDate=$(BUILD_DATE)
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
RELEASE_ARTIFACTS := $(foreach platform,$(RELEASE_PLATFORMS),$(DIST_DIR)/$(BINARY)-$(subst /,-,$(platform)))
GH ?= gh
RELEASE_TAG ?= $(VERSION)
GH_RELEASE_TITLE ?= $(BINARY) $(RELEASE_TAG)
GH_RELEASE_NOTES ?= Automated release for $(RELEASE_TAG)
GH_RELEASE_NOTES_FILE ?=
GH_RELEASE_FLAGS ?=
DOCS_SOURCE ?= docs/ktl_features_ru.md
DOCS_PDF ?= $(DIST_DIR)/ktl_features_ru.pdf
DOCS_HTML ?= $(DIST_DIR)/ktl_features_ru.html
PANDOC ?= pandoc
PDF_ENGINE ?= xelatex
MERMAID_FILTER ?= mermaid-filter
BUF_VERSION ?= v1.61.0
BUF ?= $(GO) run github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
GO_TEST_FLAGS ?= $(GOFLAGS)
REMOTE ?= origin
RELEASE_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
CHANGELOG_FILE ?= $(DIST_DIR)/CHANGELOG-$(RELEASE_TAG).md
PREVIOUS_TAG ?= $(shell git describe --tags --abbrev=0 HEAD~1 2>/dev/null)

CAPTURE_BINARY ?= capture
CAPTURE_PKG ?= ./cmd/capture

.DEFAULT_GOAL := help

.PHONY: help build build-% install release gh-release tag-release push-release changelog test test-short test-integration fmt lint tidy verify docs proto proto-lint clean loc print-%
 
PACKAGE_IMAGE ?= ktl-packager
PACKAGE_PLATFORMS ?= linux/amd64

help: ## Show this help menu
	@echo "Available targets:"
	@LC_ALL=C grep -hE '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS=":.*## "} {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

build: ## Build ktl for the current platform into ./bin/ktl
	@echo ">> building $(BINARY) for $(shell $(GO) env GOOS)/$(shell $(GO) env GOARCH)"
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

build-capture: ## Build capture for the current platform into ./bin/capture
	@echo ">> building $(CAPTURE_BINARY) for $(shell $(GO) env GOOS)/$(shell $(GO) env GOARCH)"
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(CAPTURE_BINARY) $(CAPTURE_PKG)

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

release: ## Cross-build release artifacts into ./dist
	@echo ">> building release artifacts for: $(RELEASE_PLATFORMS)"
	@mkdir -p $(DIST_DIR)
	@for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		out="$(DIST_DIR)/$(BINARY)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
		echo "   - $$os/$$arch -> $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $$out $(PKG); \
	done

gh-release: release ## Publish release artifacts to GitHub via gh CLI
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
	$(GH) release create $(RELEASE_TAG) $(RELEASE_ARTIFACTS) --title "$(GH_RELEASE_TITLE)" $$notes_flag "$$notes_value" $(GH_RELEASE_FLAGS)

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

docs: $(DOCS_PDF) $(DOCS_HTML) ## Build Russian feature guide PDF and HTML outputs

$(DOCS_PDF): $(DOCS_SOURCE) docs/custom-header.tex docs/titlepage.tex
	@mkdir -p $(DIST_DIR)
	$(PANDOC) $(DOCS_SOURCE) \
		--from markdown+yaml_metadata_block+grid_tables+pipe_tables \
		--template eisvogel \
		--table-of-contents --toc-depth 3 \
		--number-sections --highlight-style tango \
		--pdf-engine=$(PDF_ENGINE) --variable papersize=a4 \
		--include-in-header=docs/custom-header.tex \
		--include-before-body=docs/titlepage.tex \
		-o $@

$(DOCS_HTML): $(DOCS_SOURCE)
	@mkdir -p $(DIST_DIR)
	$(PANDOC) $(DOCS_SOURCE) \
		-t html5 --filter $(MERMAID_FILTER) \
		-o $@

proto: ## Generate gRPC/protobuf stubs under pkg/api
	$(BUF) generate

proto-lint: ## Lint protobuf definitions
	$(BUF) lint

deps: ## Regenerate docs/deps.md (generated package dependency map)
	@mkdir -p docs
	@./tools/gen-deps-md.py > docs/deps.md

clean: ## Remove build artifacts (bin/ and dist/)
	rm -rf $(BIN_DIR) $(DIST_DIR)

# ----- METRICS -----
loc: ## Count Go lines of code (excluding vendor/ and bin/)
	@echo ">> Counting Go LOC (excluding vendor/ and bin/)"
	@find . -type f -name '*.go' ! -path "./vendor/*" ! -path "./$(BIN_DIR)/*" -exec cat {} + | wc -l

print-%: ## Print the value of any Makefile variable
	@printf '%s=%s\n' '$*' '$($*)'

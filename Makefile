.PHONY: build build-etl build-doc build-watch build-search build-reflect build-skill build-graph build-env clean test vet fmt lint \
       dist dist-doc dist-env dist-etl dist-watch dist-search dist-reflect dist-skill dist-graph vulncheck \
       install install-doc install-env install-etl install-watch install-search install-reflect install-skill install-graph \
       install-hooks install-tools gen-stats check fmt-check test-install test-curl-install \
       fixtures verify-fixtures \
       fmt-staged vulncheck-if-deps-changed autodoc-fix skills-sync beads-sync pre-commit

BUILD_DIR := bin

# Checked-in co-change snapshot fixture (auto-search)
FIXTURE_DIR := auto-search/testdata/fixtures/auto-stack-snapshot
DIST_DIR  := dist
INSTALL_DIR ?= $(HOME)/.local/bin

PROJECTS := auto-doc auto-env auto-etl auto-watch auto-search auto-reflect auto-skill auto-graph

# Binary name and entry point per project
auto-doc_BIN   := autodoc
auto-doc_ENTRY := ./cmd/autodoc
auto-etl_BIN   := autoetl
auto-etl_ENTRY := .
auto-watch_BIN   := autowatch
auto-watch_ENTRY := ./cmd/autowatch
auto-search_BIN   := autosearch
auto-search_ENTRY := ./cmd/autosearch
auto-reflect_BIN   := autoreflect
auto-reflect_ENTRY := ./cmd/autoreflect
auto-env_BIN   := autoenv
auto-env_ENTRY := ./cmd/autoenv
auto-skill_BIN   := autoskill
auto-skill_ENTRY := ./cmd/autoskill
auto-graph_BIN   := autograph
auto-graph_ENTRY := ./cmd/autograph

# Platform defaults (overridable for cross-compilation)
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
SUFFIX ?= $(GOOS)-$(GOARCH)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X github.com/mistakenot/auto-shared/version.Version=$(VERSION)

# --- Local build ---

build: $(addprefix build-,$(subst auto-,,$(PROJECTS)))
	@echo "All binaries built in ./$(BUILD_DIR)/"

build-env:
	cd auto-env && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autoenv $(auto-env_ENTRY)

build-etl:
	cd auto-etl && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autoetl $(auto-etl_ENTRY)

build-doc:
	cd auto-doc && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autodoc $(auto-doc_ENTRY)

build-watch:
	cd auto-watch && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autowatch $(auto-watch_ENTRY)

build-search:
	cd auto-search && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autosearch $(auto-search_ENTRY)

build-reflect:
	cd auto-reflect && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autoreflect $(auto-reflect_ENTRY)

build-skill:
	cd auto-skill && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autoskill $(auto-skill_ENTRY)

build-graph:
	cd auto-graph && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autograph $(auto-graph_ENTRY)

# --- Release cross-compile (produces dist/<binary>-<suffix>) ---

dist: $(addprefix dist-,$(subst auto-,,$(PROJECTS)))
	@echo "Release binaries in ./$(DIST_DIR)/"

dist-env:
	@mkdir -p $(DIST_DIR)
	cd auto-env && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autoenv-$(SUFFIX) $(auto-env_ENTRY)

dist-doc:
	@mkdir -p $(DIST_DIR)
	cd auto-doc && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autodoc-$(SUFFIX) $(auto-doc_ENTRY)

dist-etl:
	@mkdir -p $(DIST_DIR)
	cd auto-etl && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autoetl-$(SUFFIX) $(auto-etl_ENTRY)

dist-watch:
	@mkdir -p $(DIST_DIR)
	cd auto-watch && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autowatch-$(SUFFIX) $(auto-watch_ENTRY)

dist-search:
	@mkdir -p $(DIST_DIR)
	cd auto-search && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autosearch-$(SUFFIX) $(auto-search_ENTRY)

dist-reflect:
	@mkdir -p $(DIST_DIR)
	cd auto-reflect && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autoreflect-$(SUFFIX) $(auto-reflect_ENTRY)

dist-skill:
	@mkdir -p $(DIST_DIR)
	cd auto-skill && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autoskill-$(SUFFIX) $(auto-skill_ENTRY)

dist-graph:
	@mkdir -p $(DIST_DIR)
	cd auto-graph && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/autograph-$(SUFFIX) $(auto-graph_ENTRY)

# --- Quality ---

fmt:
	@for d in $(PROJECTS); do \
		if command -v goimports >/dev/null 2>&1; then \
			(cd "$$d" && goimports -w $$(find . -name '*.go' -not -path './vendor/*')); \
		else \
			(cd "$$d" && gofmt -w $$(find . -name '*.go' -not -path './vendor/*')); \
		fi; \
	done
	@echo "All Go files formatted"

fmt-check:
	@fail=0; for d in $(PROJECTS); do \
		unformatted=$$(cd "$$d" && gofmt -l .); \
		if [ -n "$$unformatted" ]; then \
			echo "=== unformatted in $$d ==="; echo "$$unformatted"; fail=1; \
		fi; \
	done; \
	if [ $$fail -eq 1 ]; then exit 1; fi
	@echo "All Go files formatted"

lint:
	@for d in $(PROJECTS); do \
		echo "=== lint $$d ==="; \
		(cd "$$d" && golangci-lint run ./...) || exit 1; \
	done
	@echo "All projects passed lint"

vet:
	@for d in $(PROJECTS); do \
		echo "=== vet $$d ==="; \
		(cd "$$d" && go vet ./...) || exit 1; \
	done
	@echo "All projects passed vet"

vulncheck:
	@for d in $(PROJECTS); do \
		echo "=== vulncheck $$d ==="; \
		(cd "$$d" && govulncheck ./...) || exit 1; \
	done
	@echo "All projects passed vulncheck"

check: fmt-check vet lint
	@echo "All checks passed"

# --- Test ---

test: verify-fixtures
	@for d in $(PROJECTS); do \
		echo "=== test $$d ==="; \
		(cd "$$d" && go test ./...) || exit 1; \
	done

# --- Co-change snapshot fixtures ---

# Regenerate the checked-in co-change snapshot fixture from this repo's own git
# history under an isolated HOME (the developer's real ~/.auto is untouched).
fixtures:
	cd auto-search && go run ./internal/cochange/fixturegen -repo "$(CURDIR)"
	@echo "Fixtures regenerated under $(FIXTURE_DIR)/"

# Privacy guard (AC-20) + size budget (AC-16): assert no forbidden datasets /
# columns leaked into the fixture and the total checked-in size is < 1 MB.
verify-fixtures:
	cd auto-search && go run ./internal/cochange/fixturegen -verify -repo "$(CURDIR)"
	@size=$$(du -sk "$(FIXTURE_DIR)" | cut -f1); \
	echo "fixture size: $${size} KiB"; \
	if [ "$$size" -ge 1024 ]; then \
		echo "ERROR: fixture exceeds 1 MB budget ($${size} KiB)"; exit 1; \
	fi; \
	echo "fixture size budget: OK (< 1 MB)"

# --- Install ---

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/autodoc $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autoenv $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autoetl $(INSTALL_DIR)/
	@err=$$(mktemp); \
	if ! cp $(BUILD_DIR)/autowatch $(INSTALL_DIR)/ 2>$$err; then \
		if grep -qi "text file busy" $$err; then \
			echo "autowatch install skipped: destination binary is busy (text file busy)"; \
		else \
			cat $$err >&2; \
			rm -f $$err; \
			exit 1; \
		fi; \
	fi; \
	rm -f $$err
	cp $(BUILD_DIR)/autosearch $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autoreflect $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autoskill $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autograph $(INSTALL_DIR)/
	@echo "Installed to $(INSTALL_DIR)/"

# Per-binary install (build + copy a single binary). Useful for fast iteration
# on one subproject.
install-doc: build-doc
	@mkdir -p $(INSTALL_DIR) && cp $(BUILD_DIR)/autodoc $(INSTALL_DIR)/ && echo "Installed autodoc to $(INSTALL_DIR)/autodoc"

install-env: build-env
	@mkdir -p $(INSTALL_DIR) && cp $(BUILD_DIR)/autoenv $(INSTALL_DIR)/ && echo "Installed autoenv to $(INSTALL_DIR)/autoenv"

install-etl: build-etl
	@mkdir -p $(INSTALL_DIR) && cp $(BUILD_DIR)/autoetl $(INSTALL_DIR)/ && echo "Installed autoetl to $(INSTALL_DIR)/autoetl"

# autowatch may be running; tolerate "text file busy" on overwrite.
install-watch: build-watch
	@mkdir -p $(INSTALL_DIR); \
	err=$$(mktemp); \
	if ! cp $(BUILD_DIR)/autowatch $(INSTALL_DIR)/ 2>$$err; then \
		if grep -qi "text file busy" $$err; then \
			echo "autowatch install skipped: destination binary is busy (text file busy)"; \
		else \
			cat $$err >&2; rm -f $$err; exit 1; \
		fi; \
	else \
		echo "Installed autowatch to $(INSTALL_DIR)/autowatch"; \
	fi; \
	rm -f $$err

install-search: build-search
	@mkdir -p $(INSTALL_DIR) && cp $(BUILD_DIR)/autosearch $(INSTALL_DIR)/ && echo "Installed autosearch to $(INSTALL_DIR)/autosearch"

install-reflect: build-reflect
	@mkdir -p $(INSTALL_DIR) && cp $(BUILD_DIR)/autoreflect $(INSTALL_DIR)/ && echo "Installed autoreflect to $(INSTALL_DIR)/autoreflect"

install-skill: build-skill
	@mkdir -p $(INSTALL_DIR) && cp $(BUILD_DIR)/autoskill $(INSTALL_DIR)/ && echo "Installed autoskill to $(INSTALL_DIR)/autoskill"

install-graph: build-graph
	@mkdir -p $(INSTALL_DIR) && cp $(BUILD_DIR)/autograph $(INSTALL_DIR)/ && echo "Installed autograph to $(INSTALL_DIR)/autograph"

test-install:
	./e2e/test-install.sh

test-curl-install:
	./e2e/test-curl-install.sh

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

# Install developer tooling used by the pre-commit pipeline and CI. Targets
# $(INSTALL_DIR) so tools land alongside the project binaries — ~/.local/bin is
# on PATH for typical dev shells and GitHub Actions runners, whereas the
# default $(go env GOPATH)/bin often isn't.
install-tools:
	@mkdir -p $(INSTALL_DIR)
	@if ! command -v goimports >/dev/null 2>&1; then \
		echo "installing goimports..."; \
		GOBIN=$(INSTALL_DIR) go install golang.org/x/tools/cmd/goimports@latest; \
	fi
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "installing govulncheck..."; \
		GOBIN=$(INSTALL_DIR) go install golang.org/x/vuln/cmd/govulncheck@latest; \
	fi
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "installing golangci-lint..."; \
		GOBIN=$(INSTALL_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest; \
	fi
	@echo "Developer tooling ready"

install-hooks: install-tools
	git config core.hooksPath hooks/
	@echo "Git hooks installed (hooks/)"

gen-stats:
	cd auto-etl && go run ./cmd/genstats .tmp/claude/projects .tmp/stats.json

# --- Pre-commit pipeline ---
# Single entry point for the git pre-commit hook. Each sub-target is also
# runnable standalone for debugging. Build + test are intentionally excluded
# (CI runs those); pre-commit covers formatting, lint, vet, vulncheck (gated on
# go.sum changes), fixture privacy guard, and repo housekeeping.

# Auto-format staged .go files in place and re-stage them. Falls back to gofmt
# if goimports isn't installed. Re-staging keeps formatting changes inside the
# commit being made.
fmt-staged:
	@staged=$$(git diff --cached --name-only --diff-filter=ACM -- '*.go' || true); \
	if [ -n "$$staged" ]; then \
		echo "$$staged" | while IFS= read -r f; do \
			[ -f "$$f" ] || continue; \
			if command -v goimports >/dev/null 2>&1; then \
				goimports -w "$$f"; \
			else \
				gofmt -w "$$f"; \
			fi; \
		done; \
		echo "$$staged" | xargs git add; \
		echo "pre-commit: goimports/gofmt applied"; \
	fi

# Run vulncheck only when staged changes include go.mod / go.sum — vulns enter
# the repo through dependency updates, so checking on every commit is wasteful.
vulncheck-if-deps-changed:
	@if git diff --cached --name-only --diff-filter=ACMR | grep -qE '(^|/)go\.(sum|mod)$$'; then \
		if ! command -v govulncheck >/dev/null 2>&1; then \
			echo "pre-commit: go.sum/go.mod changed but govulncheck missing — run 'make install-tools'"; \
			exit 1; \
		fi; \
		echo "pre-commit: go.sum/go.mod changed — running vulncheck"; \
		$(MAKE) --no-print-directory vulncheck; \
	else \
		echo "pre-commit: vulncheck skipped (no go.sum/go.mod changes)"; \
	fi

# autodoc fix reports doc-code drift; a non-zero exit means it found issues that
# need agent attention, not a hard build failure. Print honestly and continue.
autodoc-fix:
	@if command -v autodoc >/dev/null 2>&1; then \
		if autodoc fix; then \
			echo "pre-commit: autodoc fix — no issues"; \
		else \
			echo "pre-commit: autodoc fix found issues (informational; commit allowed)"; \
		fi; \
	fi

# Reinstall locally authored skills if anything under skills/ was staged.
skills-sync:
	@staged=$$(git diff --cached --name-only --diff-filter=ACMR -- 'skills/' || true); \
	if [ -n "$$staged" ] && command -v npx >/dev/null 2>&1; then \
		npx skills install "$(CURDIR)/skills" -y 2>/dev/null || true; \
		git add "$(CURDIR)/.agents/" 2>/dev/null || true; \
		echo "pre-commit: skills synced"; \
	fi

# Flush beads issue state to JSONL so issue changes land in the commit.
beads-sync:
	@if command -v br >/dev/null 2>&1 && [ -d "$(CURDIR)/.beads" ]; then \
		br sync --flush-only --quiet 2>/dev/null || true; \
		git add "$(CURDIR)/.beads/" 2>/dev/null || true; \
		echo "pre-commit: beads synced"; \
	fi

# Pre-commit pipeline. Excludes `build` + `test` (CI runs those). Format first
# so subsequent checks see formatted code, then run all check-style gates,
# then repo housekeeping.
pre-commit: fmt-staged check verify-fixtures vulncheck-if-deps-changed autodoc-fix skills-sync beads-sync
	@echo "pre-commit: all checks passed"

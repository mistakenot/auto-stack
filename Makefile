.PHONY: build clean test vet fmt lint dist vulncheck install \
       install-hooks install-tools gen-stats check fmt-check stale-refs test-install test-curl-install \
       fixtures verify-fixtures ui-serve \
       fmt-staged vulncheck-if-deps-changed autodoc-fix skills-sync beads-sync pre-commit

BUILD_DIR := bin

# Checked-in co-change snapshot fixture (auto-search)
FIXTURE_DIR := auto-search/testdata/fixtures/auto-stack-snapshot
DIST_DIR  := dist
INSTALL_DIR ?= $(HOME)/.local/bin

# All modules participate in the quality/test loops (fmt/vet/lint/vulncheck/test).
# The single `auto` binary is built from the auto-cli umbrella module.
PROJECTS := auto-doc auto-env auto-etl auto-watch auto-search auto-reflect auto-skill auto-graph auto-ui auto-config auto-cli

# Platform defaults (overridable for cross-compilation)
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
SUFFIX ?= $(GOOS)-$(GOARCH)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X github.com/mistakenot/auto-shared/version.Version=$(VERSION)

# --- Local build (single merged binary) ---

build:
	@mkdir -p $(BUILD_DIR)
	cd auto-cli && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/auto ./cmd/auto
	@echo "Built ./$(BUILD_DIR)/auto"

# --- Local UI over Tailscale (tailnet-only, for examining the dashboard) ---
# Serves `auto ui` locally and exposes it via `tailscale serve`. Defaults to dev
# mode (live-from-disk assets) on an uncommon port. Override with PORT=/DEV=0.
# Ctrl-C tears it down.

UI_PORT ?= 8723
UI_DEV  ?= 1

ui-serve:
	PORT=$(UI_PORT) DEV=$(UI_DEV) scripts/ui-tailscale-serve.sh

# --- Release cross-compile (produces dist/auto-<suffix>) ---

dist:
	@mkdir -p $(DIST_DIR)
	cd auto-cli && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/auto-$(SUFFIX) ./cmd/auto
	@echo "Built ./$(DIST_DIR)/auto-$(SUFFIX)"

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

check: fmt-check vet lint stale-refs
	@echo "All checks passed"

# AC-7 guard: fail if any shipped string still invokes an old per-tool binary
# name (autodoc, autoetl, …) that no longer ships after the merge to `auto`.
stale-refs:
	./scripts/check-no-stale-binary-refs.sh

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

# Install the single merged binary. The `auto watch` daemon may be running, so
# tolerate "text file busy" on overwrite (install.sh removes the old inode for
# the curl path; here we surface a clear hint instead).
install: build
	@mkdir -p $(INSTALL_DIR); \
	err=$$(mktemp); \
	if ! cp $(BUILD_DIR)/auto $(INSTALL_DIR)/ 2>$$err; then \
		if grep -qi "text file busy" $$err; then \
			echo "auto install skipped: destination binary is busy (text file busy). Stop the running 'auto watch' daemon and retry."; \
		else \
			cat $$err >&2; rm -f $$err; exit 1; \
		fi; \
	else \
		echo "Installed auto to $(INSTALL_DIR)/auto"; \
	fi; \
	rm -f $$err

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
	@if command -v auto >/dev/null 2>&1; then \
		if auto doc fix; then \
			echo "pre-commit: auto doc fix — no issues"; \
		else \
			echo "pre-commit: auto doc fix found issues (informational; commit allowed)"; \
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

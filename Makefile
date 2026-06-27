.PHONY: build clean test test-race vet fmt lint dist vulncheck install \
       install-hooks install-tools gen-stats check fmt-check stale-refs test-install test-curl-install \
       fixtures verify-fixtures ui-serve \
       fmt-staged lint-staged vulncheck-if-deps-changed autodoc-fix skills-check beads-sync pre-commit \
       skills-sync-locked skills-update-check post-merge post-checkout pre-push

BUILD_DIR := bin

# Checked-in co-change snapshot fixture (auto-search)
FIXTURE_DIR := auto-search/testdata/fixtures/auto-stack-snapshot
DIST_DIR  := dist
INSTALL_DIR ?= $(HOME)/.local/bin

# All modules participate in the quality/test loops (fmt/vet/lint/vulncheck/test).
# The single `auto` binary is built from the auto-cli umbrella module.
PROJECTS := auto-shared auto-doc auto-env auto-etl auto-watch auto-search auto-reflect auto-skill auto-graph auto-ui auto-config auto-cli

# Modules whose concurrency code must be exercised under the race detector.
# Kept separate from `test` because -race requires CGO_ENABLED=1 + a C compiler,
# which cgo-less local envs may lack; CI (ubuntu-latest, has gcc) runs it.
RACE_PROJECTS := auto-shared auto-watch

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

# Lint only the packages that contain staged .go files. Pre-existing lint debt
# in untouched code (or a sibling worktree sharing the golangci cache) must not
# block a commit; CI still runs the full `make lint`. Staged files are grouped
# by sub-project, then by package directory within each project.
lint-staged:
	@staged=$$(git diff --cached --name-only --diff-filter=ACM -- '*.go' || true); \
	if [ -z "$$staged" ]; then \
		echo "pre-commit: no staged Go files — lint skipped"; \
	else \
		fail=0; \
		for proj in $$(echo "$$staged" | cut -d/ -f1 | sort -u); do \
			[ -f "$$proj/go.mod" ] || continue; \
			pkgs=$$(echo "$$staged" | grep "^$$proj/" | sed "s|^$$proj/||" \
				| xargs -n1 dirname | sort -u | sed 's|^\.$$|.|; t; s|^|./|'); \
			[ -n "$$pkgs" ] || continue; \
			echo "=== lint-staged $$proj: $$pkgs ==="; \
			(cd "$$proj" && golangci-lint run $$pkgs) || fail=1; \
		done; \
		if [ $$fail -ne 0 ]; then \
			echo "pre-commit: lint failed on staged packages"; exit 1; \
		fi; \
		echo "pre-commit: staged packages passed lint"; \
	fi

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

# Run the race detector over the concurrency-heavy modules. Requires
# CGO_ENABLED=1 + a C compiler (gcc/clang). Kept out of `test`'s deps so plain
# `make test` stays fast and works in cgo-less envs; CI runs this as a separate
# required step.
test-race:
	@for d in $(RACE_PROJECTS); do \
		echo "=== test-race $$d ==="; \
		(cd "$$d" && CGO_ENABLED=1 go test -race ./...) || exit 1; \
	done
	@echo "All race-projects passed -race"

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

# Install the single merged binary. The `auto watch` daemon may be running and
# holding the destination inode open, so mirror install.sh: remove the old inode
# before writing (the running daemon keeps its copy, the new file gets a fresh
# inode — no "text file busy"), then restart the daemon to pick up the new binary.
install: build
	@mkdir -p $(INSTALL_DIR); \
	dest="$(INSTALL_DIR)/auto"; \
	restart=""; \
	if [ -f "$$dest" ]; then \
		if fuser "$$dest" >/dev/null 2>&1; then restart=1; fi; \
		rm -f "$$dest"; \
	fi; \
	cp $(BUILD_DIR)/auto "$$dest"; \
	chmod +x "$$dest"; \
	echo "Installed auto to $$dest"; \
	if [ -n "$$restart" ]; then \
		echo "auto binary was running during install (likely the 'auto watch' daemon)."; \
		if command -v systemctl >/dev/null 2>&1 && systemctl --user is-active --quiet autowatch.service; then \
			if systemctl --user restart autowatch.service; then \
				echo "  restarted auto watch (user) daemon"; \
			else \
				echo "  could not restart user daemon — run: systemctl --user restart autowatch.service"; \
			fi; \
		else \
			echo "  restart it to pick up the new binary:"; \
			echo "    auto watch daemon restart                  # user daemon (no sudo)"; \
			echo "    sudo systemctl restart autowatch.service   # if managed by system systemd"; \
		fi; \
	fi

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

# Pre-commit skill gate: check-only, never mutates the tree. When the project
# has a native skills lock, fail the commit if any target is stale (sync --check)
# or any skill fails lint. Replaces the former npx-based skills-sync stanza.
skills-check:
	@if [ -f "$(CURDIR)/.auto/skills/lock.json" ] && command -v auto >/dev/null 2>&1; then \
		auto skill sync --check --format json || exit 1; \
		auto skill lint --format json || exit 1; \
	fi

# Post-merge / post-checkout re-materialize: reproduce the locked commit and
# render into each target without floating. Non-blocking — never fails the hook.
post-merge post-checkout: skills-sync-locked
skills-sync-locked:
	@if [ -f "$(CURDIR)/.auto/skills/lock.json" ] && command -v auto >/dev/null 2>&1; then \
		auto skill sync --locked --format json 2>/dev/null || true; \
	fi

# Pre-push upstream-drift check: opt-in, off by default. Enable per-invocation
# with SKILLS_UPDATE_CHECK=1 (or export it). Warn-only — never blocks the push.
pre-push: skills-update-check
SKILLS_UPDATE_CHECK ?= 0
skills-update-check:
	@if [ "$(SKILLS_UPDATE_CHECK)" = "1" ] && [ -f "$(CURDIR)/.auto/skills/lock.json" ] && command -v auto >/dev/null 2>&1; then \
		auto skill update --check --format json 2>/dev/null || true; \
	fi

# Flush beads issue state to JSONL so issue changes land in the commit.
beads-sync:
	@if command -v br >/dev/null 2>&1 && [ -d "$(CURDIR)/.beads" ]; then \
		br sync --flush-only --quiet 2>/dev/null || true; \
		git add "$(CURDIR)/.beads/" 2>/dev/null || true; \
		echo "pre-commit: beads synced"; \
	fi

# Pre-commit pipeline. Excludes `build` + `test` (CI runs those). Format first
# so subsequent checks see formatted code, then run the check-style gates with
# lint scoped to staged packages (full `make lint` runs in CI), then repo
# housekeeping. We inline check's gates rather than depend on `check` so we can
# substitute lint-staged for the full lint without affecting CI's `make check`.
pre-commit: fmt-staged fmt-check vet lint-staged stale-refs verify-fixtures vulncheck-if-deps-changed autodoc-fix skills-check beads-sync
	@echo "pre-commit: all checks passed"

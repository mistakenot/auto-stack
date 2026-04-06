.PHONY: build build-etl build-doc build-watch build-search clean test vet fmt lint \
       install install-hooks gen-stats check dist test-install

BUILD_DIR := bin
DIST_DIR  := dist
INSTALL_DIR ?= $(HOME)/.local/bin

PROJECTS := auto-doc auto-etl auto-watch auto-search

# Binary name and entry point per project
auto-doc_BIN   := autodoc
auto-doc_ENTRY := ./cmd/autodoc
auto-etl_BIN   := autoetl
auto-etl_ENTRY := .
auto-watch_BIN   := autowatch
auto-watch_ENTRY := ./cmd/autowatch
auto-search_BIN   := autosearch
auto-search_ENTRY := ./cmd/autosearch

# Platform defaults (overridable for cross-compilation)
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
SUFFIX ?= $(GOOS)-$(GOARCH)

# --- Local build ---

build: $(addprefix build-,$(subst auto-,,$(PROJECTS)))
	@echo "All binaries built in ./$(BUILD_DIR)/"

build-etl:
	cd auto-etl && go build -o ../$(BUILD_DIR)/autoetl $(auto-etl_ENTRY)

build-doc:
	cd auto-doc && go build -o ../$(BUILD_DIR)/autodoc $(auto-doc_ENTRY)

build-watch:
	cd auto-watch && go build -o ../$(BUILD_DIR)/autowatch $(auto-watch_ENTRY)

build-search:
	cd auto-search && go build -o ../$(BUILD_DIR)/autosearch $(auto-search_ENTRY)

# --- Release cross-compile (produces dist/<binary>-<suffix>) ---

dist: $(addprefix dist-,$(subst auto-,,$(PROJECTS)))
	@echo "Release binaries in ./$(DIST_DIR)/"

dist-doc:
	@mkdir -p $(DIST_DIR)
	cd auto-doc && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="-s -w" -o ../$(DIST_DIR)/autodoc-$(SUFFIX) $(auto-doc_ENTRY)

dist-etl:
	@mkdir -p $(DIST_DIR)
	cd auto-etl && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="-s -w" -o ../$(DIST_DIR)/autoetl-$(SUFFIX) $(auto-etl_ENTRY)

dist-watch:
	@mkdir -p $(DIST_DIR)
	cd auto-watch && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="-s -w" -o ../$(DIST_DIR)/autowatch-$(SUFFIX) $(auto-watch_ENTRY)

dist-search:
	@mkdir -p $(DIST_DIR)
	cd auto-search && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="-s -w" -o ../$(DIST_DIR)/autosearch-$(SUFFIX) $(auto-search_ENTRY)

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

check: fmt-check vet lint
	@echo "All checks passed"

# --- Test ---

test:
	@for d in $(PROJECTS); do \
		echo "=== test $$d ==="; \
		(cd "$$d" && go test ./...) || exit 1; \
	done

# --- Install ---

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/autodoc $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autoetl $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autowatch $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autosearch $(INSTALL_DIR)/
	@echo "Installed to $(INSTALL_DIR)/"

test-install:
	./e2e/test-install.sh

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

install-hooks:
	git config core.hooksPath hooks/
	@echo "Git hooks installed (hooks/)"

gen-stats:
	cd auto-etl && go run ./cmd/genstats .tmp/claude/projects .tmp/stats.json

.PHONY: build build-etl build-doc build-watch build-search clean test vet fmt lint install install-hooks gen-stats

BUILD_DIR := bin

build: build-etl build-doc build-watch build-search
	@echo "All binaries built in ./$(BUILD_DIR)/"

build-etl:
	cd auto-etl && go build -o ../$(BUILD_DIR)/autoetl .

build-doc:
	cd auto-doc && go build -o ../$(BUILD_DIR)/autodoc ./cmd/autodoc

build-watch:
	cd auto-watch && go build -o ../$(BUILD_DIR)/autowatch ./cmd/autowatch

build-search:
	cd auto-search && go build -o ../$(BUILD_DIR)/autosearch ./cmd/autosearch

clean:
	rm -rf $(BUILD_DIR)

test:
	cd auto-etl && go test ./...
	cd auto-doc && go test ./...
	cd auto-watch && go test ./...
	cd auto-search && go test ./...

fmt:
	@for d in auto-*/; do \
		if [ -f "$$d/go.mod" ]; then \
			if command -v goimports >/dev/null 2>&1; then \
				(cd "$$d" && goimports -w $$(find . -name '*.go' -not -path './vendor/*')); \
			else \
				(cd "$$d" && gofmt -w $$(find . -name '*.go' -not -path './vendor/*')); \
			fi; \
		fi; \
	done
	@echo "All Go files formatted"

lint:
	@for d in auto-*/; do \
		if [ -f "$$d/go.mod" ]; then \
			echo "=== lint $$d ==="; \
			(cd "$$d" && golangci-lint run ./...) || exit 1; \
		fi; \
	done
	@echo "All projects passed lint"

vet: lint

INSTALL_DIR ?= $(HOME)/.local/bin

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/autodoc $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autoetl $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autowatch $(INSTALL_DIR)/
	cp $(BUILD_DIR)/autosearch $(INSTALL_DIR)/
	@echo "Installed to $(INSTALL_DIR)/"

install-hooks:
	git config core.hooksPath hooks/
	@echo "Git hooks installed (hooks/)"

gen-stats:
	cd auto-etl && go run ./cmd/genstats .tmp/claude/projects .tmp/stats.json

# Makefile for torn

.PHONY: all setup build run stats test test-coverage lint fmt vet clean check

APP_NAME := torn
BIN_DIR := bin
BINARY := $(BIN_DIR)/$(APP_NAME)
CMD := ./cmd/$(APP_NAME)

all: lint test build

# Setup local Git exclusions and hooks
setup:
	@echo "Configuring local exclusions in .git/info/exclude..."
	@mkdir -p .git/info .git/hooks
	@grep -qxF 'AGENTS.md' .git/info/exclude 2>/dev/null || echo 'AGENTS.md' >> .git/info/exclude
	@grep -qxF 'agents.md' .git/info/exclude 2>/dev/null || echo 'agents.md' >> .git/info/exclude
	@grep -qxF '.missions/' .git/info/exclude 2>/dev/null || echo '.missions/' >> .git/info/exclude
	@grep -qxF '.missions' .git/info/exclude 2>/dev/null || echo '.missions' >> .git/info/exclude
	@grep -qxF 'TREE.md' .git/info/exclude 2>/dev/null || echo 'TREE.md' >> .git/info/exclude
	@grep -qxF '.agents/' .git/info/exclude 2>/dev/null || echo '.agents/' >> .git/info/exclude
	@echo "Verifying local hooks..."
	@test -f scripts/hooks/pre-commit && test ! -f .git/hooks/pre-commit && cp scripts/hooks/pre-commit .git/hooks/pre-commit || true
	@test -f .git/hooks/commit-msg && chmod +x .git/hooks/commit-msg || true
	@test -f .git/hooks/prepare-commit-msg && chmod +x .git/hooks/prepare-commit-msg || true
	@test -f .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit || true
	@echo "Setup completed successfully."

build:
	@echo "Building $(BINARY)..."
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-s -w" -o $(BINARY) $(CMD)

run:
	@go run $(CMD)

FORMAT ?=
ARGS ?=

stats:
	@if [ -n "$(FORMAT)" ]; then \
		go run $(CMD) stats --format $(FORMAT) $(ARGS); \
	else \
		go run $(CMD) stats $(ARGS); \
	fi

test:
	@echo "Running tests..."
	go test -v -race ./...

test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

lint: fmt vet

fmt:
	@echo "Checking formatting..."
	@test -z "$$(gofmt -l . | grep -v '^.git' | tee /dev/stderr)" || (echo "Code is not formatted. Run: gofmt -w ." && exit 1)

vet:
	@echo "Running go vet..."
	go vet ./...

check: lint test

clean:
	@echo "Cleaning artifacts..."
	rm -rf $(BIN_DIR) coverage.out coverage.html

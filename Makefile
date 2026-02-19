APP       := ai-mr-comment
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS   := -ldflags="-s -w -X 'main.Version=$(VERSION)'"
BUILD_DIR := dist
PLATFORMS := linux/amd64 darwin/amd64 darwin/arm64 windows/amd64

# Maximum allowed binary size in bytes for linux/amd64 release build (35 MB)
# Current baseline ~23.4 MB; gRPC+protobuf from generative-ai-go dominate.
# Raise this ceiling deliberately if you add large deps; shrink it to lock in gains.
MAX_BINARY_BYTES := 36700160

.PHONY: all clean build release test test-cover test-integration test-fuzz lint test-run install install-completion-bash install-completion-zsh check-size help

all: build

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-26s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build binary to dist/
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .

check-size: ## Verify linux/amd64 binary is within the size limit
	@mkdir -p $(BUILD_DIR)
	@echo "Building linux/amd64 for size check..."
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP)-size-check .
	@SIZE=$$(wc -c < $(BUILD_DIR)/$(APP)-size-check); \
	rm -f $(BUILD_DIR)/$(APP)-size-check; \
	SIZE_MB=$$(echo "scale=1; $$SIZE / 1048576" | bc); \
	MAX_MB=$$(echo "scale=1; $(MAX_BINARY_BYTES) / 1048576" | bc); \
	echo "Binary size (linux/amd64): $${SIZE_MB} MB (limit: $${MAX_MB} MB)"; \
	if [ "$$SIZE" -gt "$(MAX_BINARY_BYTES)" ]; then \
		echo "ERROR: binary exceeds size limit ($${SIZE_MB} MB > $${MAX_MB} MB)"; \
		exit 1; \
	else \
		echo "OK: binary is within size limit"; \
	fi

run: ## Build and run against current git diff
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment

run-debug: ## Build and run with --debug flag
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment --debug

test: ## Run unit tests
	go test -v ./...

test-cover: ## Run tests with coverage report
	go test -v -coverprofile=coverage.out ./...

test-integration: ## Run integration tests (requires GEMINI_API_KEY)
	go test -v -tags=integration ./...

test-fuzz: ## Run fuzz tests (30s per target)
	go test -fuzz=FuzzSplitDiffByFile -fuzztime=30s .
	go test -fuzz=FuzzProcessDiff -fuzztime=30s .
	go test -fuzz=FuzzEstimateCost -fuzztime=30s .

lint: ## Run golangci-lint
	golangci-lint run ./...

PROVIDER ?= gemini

test-run: build ## Build and run on current diff with PROVIDER (default: gemini)
	@echo "Running ai-mr-comment on current git diff with provider: $(PROVIDER)..."
	./dist/ai-mr-comment --provider $(PROVIDER)

install: ## Install binary via go install
	go install $(LDFLAGS) .

install-completion-bash: build ## Generate bash completion script to /tmp/
	./dist/ai-mr-comment completion bash > /tmp/ai-mr-comment-completion.bash
	@echo "Source this file or move it to your bash completion directory:"
	@echo "  source /tmp/ai-mr-comment-completion.bash"

install-completion-zsh: build ## Generate zsh completion script to /tmp/
	./dist/ai-mr-comment completion zsh > /tmp/_ai-mr-comment
	@echo "Move to your zsh functions path, e.g.:"
	@echo "  mv /tmp/_ai-mr-comment ~/.zsh/completions/_ai-mr-comment"

clean: ## Remove build artifacts and coverage output
	rm -rf $(BUILD_DIR) coverage.out

release: clean ## Build release binaries for all platforms
	@mkdir -p $(BUILD_DIR)
	@for PLATFORM in $(PLATFORMS); do \
		OS=$${PLATFORM%%/*}; ARCH=$${PLATFORM##*/}; \
		EXT=$$( [ "$$OS" = "windows" ] && echo .exe || echo ); \
		OUTPUT=$(BUILD_DIR)/$(APP)-$$OS-$$ARCH$$EXT; \
		echo "Building: $$OUTPUT"; \
		GOOS=$$OS GOARCH=$$ARCH go build $(LDFLAGS) -o $$OUTPUT .; \
	done

APP       := ai-mr-comment
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS   := -ldflags="-s -w -X 'main.Version=$(VERSION)'"
BUILD_DIR := dist
PLATFORMS := linux/amd64 darwin/amd64 darwin/arm64 windows/amd64

# Maximum allowed binary size in bytes for linux/amd64 release build (35 MB)
# Current baseline ~23.4 MB; gRPC+protobuf from generative-ai-go dominate.
# Raise this ceiling deliberately if you add large deps; shrink it to lock in gains.
MAX_BINARY_BYTES := 36700160

.PHONY: all clean build release test test-cover test-integration test-integration-ollama test-fuzz lint test-run quick-commit run-debug changelog gen-aliases install install-completion-bash install-completion-zsh check-size help docker-build docker-run docker-quick-commit profile-cpu profile-mem profile-bench

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

ARGS ?=

run: ## Build and run against current git diff (pass extra flags with ARGS="--flag value")
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment $(ARGS)

quick-commit: ## Build and run quick-commit (pass extra flags with ARGS="--dry-run")
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment quick-commit $(ARGS)

COMMIT_RANGE ?= HEAD~10..HEAD

changelog: ## Build and generate a changelog entry (COMMIT_RANGE="v1.2.0..HEAD" ARGS="--provider gemini")
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment changelog --commit="$(COMMIT_RANGE)" $(ARGS)

gen-aliases: ## Print shell aliases for ai-mr-comment (append to ~/.bashrc or ~/.zshrc)
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment gen-aliases

run-debug: ## Build and run with --debug flag
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment --debug

test: ## Run unit tests
	go test -v ./...

test-cover: ## Run tests with coverage report
	go test -v -coverprofile=coverage.out ./...

test-integration: ## Run all integration tests (provider tests may skip if env vars are missing)
	go test -v -tags=integration ./...

INTEGRATION_TEST_PATTERN ?= ^TestIntegration_Ollama

test-integration-ollama: ## Run only Ollama integration tests (set OLLAMA_MODEL/OLLAMA_ENDPOINT as needed)
	go test -v -tags=integration -run '$(INTEGRATION_TEST_PATTERN)' ./...

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

DOCKER_IMAGE ?= ai-mr-comment
DOCKER_TAG   ?= latest

# Common docker run flags:
#   -it                           interactive terminal (for streaming output)
#   --rm                          remove container on exit
#   -v $(PWD):/repo               mount current repo so git diffs work
#   -v ~/.ai-mr-comment.toml:...  optional: mount config file
#   -e OPENAI_API_KEY=...         pass API key from host env
DOCKER_RUN_FLAGS ?= \
  -it --rm \
  -v "$(PWD):/repo" \
  -w /repo \
  -e OPENAI_API_KEY \
  -e ANTHROPIC_API_KEY \
  -e GEMINI_API_KEY \
  -e GITHUB_TOKEN \
  -e GITLAB_TOKEN

docker-build: ## Build the Docker image (IMAGE=name TAG=tag)
	docker build \
		--build-arg VERSION=$(VERSION) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run: docker-build ## Build image and run with current repo mounted (ARGS="--provider openai")
	docker run $(DOCKER_RUN_FLAGS) \
		$(shell [ -f ~/.ai-mr-comment.toml ] && echo '-v $(HOME)/.ai-mr-comment.toml:/home/aiuser/.ai-mr-comment.toml:ro') \
		$(DOCKER_IMAGE):$(DOCKER_TAG) $(ARGS)

docker-quick-commit: docker-build ## Build image and run quick-commit with current repo mounted (ARGS="--dry-run")
	docker run $(DOCKER_RUN_FLAGS) \
		$(shell [ -f ~/.ai-mr-comment.toml ] && echo '-v $(HOME)/.ai-mr-comment.toml:/home/aiuser/.ai-mr-comment.toml:ro') \
		$(DOCKER_IMAGE):$(DOCKER_TAG) quick-commit $(ARGS)

PROFILE_DIR ?= dist/profiles

profile-cpu: ## CPU profile of unit tests (opens pprof tool — requires graphviz for svg)
	@mkdir -p $(PROFILE_DIR)
	go test -cpuprofile=$(PROFILE_DIR)/cpu.prof -run='^$$' -bench=. ./... 2>/dev/null || \
	  go test -cpuprofile=$(PROFILE_DIR)/cpu.prof ./...
	@echo "CPU profile written to $(PROFILE_DIR)/cpu.prof"
	@echo "Inspect with:  go tool pprof $(PROFILE_DIR)/cpu.prof"
	@echo "  (top, web, list <func>, png > cpu.png)"

profile-mem: ## Memory (heap) profile of unit tests
	@mkdir -p $(PROFILE_DIR)
	go test -memprofile=$(PROFILE_DIR)/mem.prof -memprofilerate=1 ./...
	@echo "Memory profile written to $(PROFILE_DIR)/mem.prof"
	@echo "Inspect with:  go tool pprof $(PROFILE_DIR)/mem.prof"

profile-bench: ## Run benchmarks and capture both CPU and memory profiles
	@mkdir -p $(PROFILE_DIR)
	go test \
	  -run='^$$' \
	  -bench=. \
	  -benchmem \
	  -cpuprofile=$(PROFILE_DIR)/bench-cpu.prof \
	  -memprofile=$(PROFILE_DIR)/bench-mem.prof \
	  ./...
	@echo "Benchmark profiles written to $(PROFILE_DIR)/"
	@echo "CPU:  go tool pprof $(PROFILE_DIR)/bench-cpu.prof"
	@echo "Mem:  go tool pprof $(PROFILE_DIR)/bench-mem.prof"
	@echo "Open interactive browser UI with:  go tool pprof -http=:6060 $(PROFILE_DIR)/bench-cpu.prof"

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

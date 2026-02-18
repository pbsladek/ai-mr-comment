APP       := ai-mr-comment
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS   := -ldflags="-s -w -X 'main.Version=$(VERSION)'"
BUILD_DIR := dist
PLATFORMS := linux/amd64 darwin/amd64 darwin/arm64 windows/amd64

# Maximum allowed binary size in bytes for linux/amd64 release build (35 MB)
# Current baseline ~23.4 MB; gRPC+protobuf from generative-ai-go dominate.
# Raise this ceiling deliberately if you add large deps; shrink it to lock in gains.
MAX_BINARY_BYTES := 36700160
NEXT_VERSION := $(shell \
  git fetch --tags >/dev/null 2>&1; \
  latest=$$(git tag --sort=-v:refname | grep '^v[0-9]' | head -n1); \
  if [ -z "$$latest" ]; then echo v0.0.1; \
  else \
    major=$$(echo $$latest | cut -d. -f1 | tr -d 'v'); \
    minor=$$(echo $$latest | cut -d. -f2); \
    patch=$$(echo $$latest | cut -d. -f3); \
    echo v$$major.$$minor.$$((patch + 1)); \
  fi \
)

.PHONY: all clean build release test test-cover test-integration lint test-run install install-completion-bash install-completion-zsh check-size

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .

check-size:
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

run:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment

run-debug:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .
	./dist/ai-mr-comment --debug

test:
	go test -v ./...

test-cover:
	go test -v -coverprofile=coverage.out ./...

test-integration:
	go test -v -tags=integration ./...

lint:
	golangci-lint run ./...

PROVIDER ?= gemini

test-run: build
	@echo "Running ai-mr-comment on current git diff with provider: $(PROVIDER)..."
	./dist/ai-mr-comment --provider $(PROVIDER)

install:
	go install $(LDFLAGS) .

install-completion-bash: build
	./dist/ai-mr-comment completion bash > /tmp/ai-mr-comment-completion.bash
	@echo "Source this file or move it to your bash completion directory:"
	@echo "  source /tmp/ai-mr-comment-completion.bash"

install-completion-zsh: build
	./dist/ai-mr-comment completion zsh > /tmp/_ai-mr-comment
	@echo "Move to your zsh functions path, e.g.:"
	@echo "  mv /tmp/_ai-mr-comment ~/.zsh/completions/_ai-mr-comment"

clean:
	rm -rf $(BUILD_DIR) coverage.out

next-version:
	@echo "Next version: $(NEXT_VERSION)"

tag-release:
	@git tag $(NEXT_VERSION)
	@git push origin $(NEXT_VERSION)
	@echo "Tagged and pushed: $(NEXT_VERSION)"

release: clean
	@mkdir -p $(BUILD_DIR)
	@for PLATFORM in $(PLATFORMS); do \
		OS=$${PLATFORM%%/*}; ARCH=$${PLATFORM##*/}; \
		EXT=$$( [ "$$OS" = "windows" ] && echo .exe || echo ); \
		OUTPUT=$(BUILD_DIR)/$(APP)-$$OS-$$ARCH$$EXT; \
		echo "Building: $$OUTPUT"; \
		GOOS=$$OS GOARCH=$$ARCH go build $(LDFLAGS) -o $$OUTPUT .; \
	done

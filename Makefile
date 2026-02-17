APP       := ai-mr-comment
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS   := -ldflags="-s -w -X 'main.Version=$(VERSION)'"
BUILD_DIR := dist
PLATFORMS := linux/amd64 darwin/amd64 darwin/arm64 windows/amd64
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

.PHONY: all clean build release test test-cover lint

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .

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
	go test -cover ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)

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

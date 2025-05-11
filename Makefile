APP       := ai-mr-comment
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS   := -ldflags="-s -w -X 'main.Version=$(VERSION)'"
BUILD_DIR := dist

PLATFORMS := linux/amd64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all clean build release test test-cover

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) ./main.go

run:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) ./main.go
	./dist/ai-mr-comment

run-debug:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) ./main.go
	./dist/ai-mr-comment --debug

test:
	go test -v ./...

test-cover:
	go test -cover ./...

clean:
	rm -rf $(BUILD_DIR)

release: clean
	@mkdir -p $(BUILD_DIR)
	@for PLATFORM in $(PLATFORMS); do \
		OS=$${PLATFORM%%/*}; ARCH=$${PLATFORM##*/}; \
		EXT=$$( [ "$$OS" = "windows" ] && echo .exe || echo ); \
		OUTPUT=$(BUILD_DIR)/$(APP)-$$OS-$$ARCH$$EXT; \
		echo "Building: $$OUTPUT"; \
		GOOS=$$OS GOARCH=$$ARCH go build $(LDFLAGS) -o $$OUTPUT ./main.go; \
	done


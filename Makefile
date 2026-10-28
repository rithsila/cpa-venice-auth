UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
PLUGIN_EXT := dylib
else
PLUGIN_EXT := so
endif

# CPA v8 discovers plugins by filename: <plugins.dir>/<id>.dylib
# or nested path: <plugins.dir>/<os>/<arch>/<id>.dylib.
# The plugin ID must match the key under plugins.configs.
PLUGIN_DIR := bin/plugins
PLUGIN := $(PLUGIN_DIR)/venice-provider.$(PLUGIN_EXT)

DARWIN_AMD64_CC ?= clang -target x86_64-apple-macos
LINUX_AMD64_CC ?= x86_64-linux-gnu-gcc
LINUX_ARM64_CC ?= aarch64-linux-gnu-gcc
WINDOWS_AMD64_CC ?= x86_64-w64-mingw32-gcc

.PHONY: build darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 windows-amd64 artifacts checksums test race vet itest smoke soak clean

build:
	@mkdir -p $(PLUGIN_DIR)
	CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -o $(PLUGIN) ./cmd/plugin
	rm -f $(PLUGIN_DIR)/venice-provider.h
	@echo "built $(PLUGIN)"

darwin-arm64:
	@mkdir -p $(PLUGIN_DIR)/darwin/arm64
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -buildmode=c-shared -trimpath -o $(PLUGIN_DIR)/darwin/arm64/venice-provider.dylib ./cmd/plugin
	rm -f $(PLUGIN_DIR)/darwin/arm64/venice-provider.h
	@echo "built $(PLUGIN_DIR)/darwin/arm64/venice-provider.dylib"

darwin-amd64:
	@mkdir -p $(PLUGIN_DIR)/darwin/amd64
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="$(DARWIN_AMD64_CC)" go build -buildmode=c-shared -trimpath -o $(PLUGIN_DIR)/darwin/amd64/venice-provider.dylib ./cmd/plugin
	rm -f $(PLUGIN_DIR)/darwin/amd64/venice-provider.h
	@echo "built $(PLUGIN_DIR)/darwin/amd64/venice-provider.dylib"

linux-amd64:
	@which $(firstword $(LINUX_AMD64_CC)) >/dev/null 2>&1 || { echo "Error: $(LINUX_AMD64_CC) not found. Install cross-compiler (e.g. gcc-x86-64-linux-gnu) to build linux-amd64." >&2; exit 1; }
	@mkdir -p $(PLUGIN_DIR)/linux/amd64
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC="$(LINUX_AMD64_CC)" go build -buildmode=c-shared -trimpath -o $(PLUGIN_DIR)/linux/amd64/venice-provider.so ./cmd/plugin
	rm -f $(PLUGIN_DIR)/linux/amd64/venice-provider.h
	@echo "built $(PLUGIN_DIR)/linux/amd64/venice-provider.so"

linux-arm64:
	@which $(firstword $(LINUX_ARM64_CC)) >/dev/null 2>&1 || { echo "Error: $(LINUX_ARM64_CC) not found. Install cross-compiler (e.g. gcc-aarch64-linux-gnu) to build linux-arm64." >&2; exit 1; }
	@mkdir -p $(PLUGIN_DIR)/linux/arm64
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC="$(LINUX_ARM64_CC)" go build -buildmode=c-shared -trimpath -o $(PLUGIN_DIR)/linux/arm64/venice-provider.so ./cmd/plugin
	rm -f $(PLUGIN_DIR)/linux/arm64/venice-provider.h
	@echo "built $(PLUGIN_DIR)/linux/arm64/venice-provider.so"

windows-amd64:
	@which $(firstword $(WINDOWS_AMD64_CC)) >/dev/null 2>&1 || { echo "Error: $(WINDOWS_AMD64_CC) not found. Install mingw-w64 cross-compiler to build windows-amd64." >&2; exit 1; }
	@mkdir -p $(PLUGIN_DIR)/windows/amd64
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="$(WINDOWS_AMD64_CC)" go build -buildmode=c-shared -trimpath -o $(PLUGIN_DIR)/windows/amd64/venice-provider.dll ./cmd/plugin
	rm -f $(PLUGIN_DIR)/windows/amd64/venice-provider.h
	@echo "built $(PLUGIN_DIR)/windows/amd64/venice-provider.dll"

artifacts: build darwin-arm64 darwin-amd64 checksums

checksums:
	@mkdir -p $(PLUGIN_DIR)
	@find $(PLUGIN_DIR) -type f \( -name "*.dylib" -o -name "*.so" -o -name "*.dll" \) | sort | xargs shasum -a 256 > checksums.txt
	@echo "generated checksums.txt"

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

itest:
	./scripts/itest.sh

smoke:
	./scripts/smoke.sh

soak:
	./scripts/soak.sh

clean:
	rm -rf bin checksums.txt

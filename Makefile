UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
PLUGIN_EXT := dylib
else
PLUGIN_EXT := so
endif

# CPA v8 discovers plugins by filename: <plugins.dir>/<id>.dylib.
# The plugin ID must match the key under plugins.configs.
PLUGIN_DIR := bin/plugins
PLUGIN := $(PLUGIN_DIR)/venice-provider.$(PLUGIN_EXT)

.PHONY: build test race vet clean

build:
	@mkdir -p $(PLUGIN_DIR)
	CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -o $(PLUGIN) ./cmd/plugin
	rm -f $(PLUGIN_DIR)/venice-provider.h
	@echo "built $(PLUGIN)"

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf bin

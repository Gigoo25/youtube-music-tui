BINARY  := ytmusic
PKG     := ./cmd/ytmusic
# Version string baked into the binary (shown by `ytmusic --version`). Derives
# from git tags/commit; falls back to "dev" outside a git checkout.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build release run test vet clean

# Development build.
build:
	go build -ldflags='-X main.version=$(VERSION)' -o $(BINARY) $(PKG)

# Stripped, reproducible release build (~34% smaller: omits the symbol table and
# DWARF debug info; -trimpath removes local filesystem paths).
release:
	go build -trimpath -ldflags='-s -w -X main.version=$(VERSION)' -o $(BINARY) $(PKG)

run:
	go run $(PKG)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

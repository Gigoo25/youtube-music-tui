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
# DWARF debug info; -trimpath removes local filesystem paths). CGO_ENABLED=0
# makes the binary fully static — all deps are pure Go — so it runs on any
# Linux of the same arch (a cgo build on NixOS embeds a /nix/store interpreter
# path and won't start elsewhere).
release:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=$(VERSION)' -o $(BINARY) $(PKG)

run:
	go run $(PKG)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

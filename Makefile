BINARY := ytmusic
PKG    := ./cmd/ytmusic

.PHONY: build release run test vet clean

# Development build.
build:
	go build -o $(BINARY) $(PKG)

# Stripped, reproducible release build (~34% smaller: omits the symbol table and
# DWARF debug info; -trimpath removes local filesystem paths).
release:
	go build -trimpath -ldflags='-s -w' -o $(BINARY) $(PKG)

run:
	go run $(PKG)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

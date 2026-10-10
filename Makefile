BINARY   := ghcli
DIST     := ./dist
PKG      := github.com/EugenMayer/ghcli
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS  := -s -w -X $(PKG)/cmd.version=$(VERSION) -X $(PKG)/cmd.commit=$(COMMIT)
PREFIX   ?= /usr/local
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

export CGO_ENABLED := 0

.PHONY: all build dist install test vet fmt lint clean

all: vet test build

## build: build for the current platform into ./dist/ghcli
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## install: build and copy the binary to $(PREFIX)/bin/ghcli (uses sudo)
install: build
	sudo install -m 0755 $(DIST)/$(BINARY) $(PREFIX)/bin/$(BINARY)

## dist: cross-compile all release platforms into ./dist and write checksums
dist:
	@mkdir -p $(DIST)
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		out=$(DIST)/$(BINARY)-$$os-$$arch$$ext; \
		echo "building $$out"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out .; \
	done
	cd $(DIST) && sha256sum $(BINARY)-* > checksums.txt

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -rf $(DIST)

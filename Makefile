CMD_DESTDIR ?= /usr/local
PREFIX ?= $(CURDIR)/out/

PKG=github.com/ktock/container2wasm
VERSION=$(shell git describe --match 'v[0-9]*' --dirty='.m' --always --tags)
REVISION=$(shell git rev-parse HEAD)$(shell if ! git diff --no-ext-diff --quiet --exit-code; then echo .m; fi)
GO_EXTRA_LDFLAGS=-extldflags '-static'
GO_LD_FLAGS=-ldflags '-s -w -X $(PKG)/version.Version=$(VERSION) -X $(PKG)/version.Revision=$(REVISION) $(GO_EXTRA_LDFLAGS)'
GO_BUILDTAGS=-tags "osusergo netgo static_build"

all: c2w

build: c2w

c2w:
	CGO_ENABLED=0 go build -o $(PREFIX)/c2w $(GO_LD_FLAGS) $(GO_BUILDTAGS) -v ./cmd/c2w

install:
	install -D -m 755 $(PREFIX)/c2w $(CMD_DESTDIR)/bin

artifacts: clean
	GOOS=linux GOARCH=amd64 make c2w
	tar -C $(PREFIX) --owner=0 --group=0 -zcvf $(PREFIX)/container2wasm-$(VERSION)-linux-amd64.tar.gz c2w

	GOOS=linux GOARCH=arm64 make c2w
	tar -C $(PREFIX) --owner=0 --group=0 -zcvf $(PREFIX)/container2wasm-$(VERSION)-linux-arm64.tar.gz c2w

	rm -f $(PREFIX)/c2w

test:
	./tests/test.sh

benchmark:
	./tests/bench.sh

clean:
	rm -f $(CURDIR)/out/*

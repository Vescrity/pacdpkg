BIN := pacdpkg
PREFIX ?= /usr/local
DATADIR ?= $(PREFIX)/share
BASH_COMPDIR ?= $(DATADIR)/bash-completion/completions
ZSH_COMPDIR ?= $(DATADIR)/zsh/site-functions
GO ?= go

.PHONY: all build test vet fmt install cross clean

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/$(BIN) ./cmd/pacdpkg

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w $$($(GO) list -f '{{.Dir}}' ./...)

install: build
	install -Dm755 bin/$(BIN) $(DESTDIR)$(PREFIX)/bin/$(BIN)
	install -Dm644 completions/pacdpkg.bash $(DESTDIR)$(BASH_COMPDIR)/$(BIN)
	install -Dm644 completions/_pacdpkg $(DESTDIR)$(ZSH_COMPDIR)/_$(BIN)

cross:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '-s -w' \
		-o dist/$(BIN)-linux-amd64 ./cmd/pacdpkg
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '-s -w' \
		-o dist/$(BIN)-linux-arm64 ./cmd/pacdpkg

clean:
	rm -rf bin dist

BIN := pacdpkg
PREFIX ?= /usr/local
DATADIR ?= $(PREFIX)/share
BASH_COMPDIR ?= $(DATADIR)/bash-completion/completions
ZSH_COMPDIR ?= $(DATADIR)/zsh/site-functions
GO ?= go

.PHONY: all build test vet fmt install clean

all: build

build:
	CGO_ENABLED=1 $(GO) build -trimpath -o bin/$(BIN) ./cmd/pacdpkg

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

clean:
	rm -rf bin dist

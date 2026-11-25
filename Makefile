BIN := pacdpkg
PREFIX ?= /usr/local
DATADIR ?= $(PREFIX)/share
BASH_COMPDIR ?= $(DATADIR)/bash-completion/completions
ZSH_COMPDIR ?= $(DATADIR)/zsh/site-functions
GO ?= go

ifeq ($(origin TAGS),undefined)
ifneq ($(wildcard /usr/include/apt-pkg/depcache.h),)
TAGS := libapt
endif
endif

TAGFLAGS = $(if $(TAGS),-tags "$(TAGS)")

.PHONY: all build build-libapt test vet fmt install install-libapt clean

all: build

build:
	CGO_ENABLED=1 $(GO) build $(TAGFLAGS) -trimpath -o bin/$(BIN) ./cmd/pacdpkg

build-libapt:
	CGO_ENABLED=1 $(GO) build -tags libapt -trimpath -o bin/$(BIN) ./cmd/pacdpkg

test:
	$(GO) test $(TAGFLAGS) ./...

vet:
	$(GO) vet $(TAGFLAGS) ./...

fmt:
	gofmt -w $$($(GO) list -f '{{.Dir}}' ./...)

install: build
	install -Dm755 bin/$(BIN) $(DESTDIR)$(PREFIX)/bin/$(BIN)
	install -Dm644 completions/pacdpkg.bash $(DESTDIR)$(BASH_COMPDIR)/$(BIN)
	install -Dm644 completions/_pacdpkg $(DESTDIR)$(ZSH_COMPDIR)/_$(BIN)

install-libapt: build-libapt
	install -Dm755 bin/$(BIN) $(DESTDIR)$(PREFIX)/bin/$(BIN)
	install -Dm644 completions/pacdpkg.bash $(DESTDIR)$(BASH_COMPDIR)/$(BIN)
	install -Dm644 completions/_pacdpkg $(DESTDIR)$(ZSH_COMPDIR)/_$(BIN)

clean:
	rm -rf bin dist

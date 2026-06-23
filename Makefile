GO ?= go
PKG := ./...
BIN := feelc

.PHONY: all build test test-race vet fmt cover bench tidy clean run lint-english

all: vet test build

# lint-english fails if any non-English (French) text creeps into code/docs/UI (the repo is
# English-only). Runs as part of the normal test suite too; this target is a focused shortcut.
lint-english:
	$(GO) test ./internal/i18nguard/

build:
	$(GO) build -o $(BIN) ./cmd/feelc

test:
	$(GO) test $(PKG)

test-race:
	$(GO) test -race $(PKG)

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

cover:
	$(GO) test -coverprofile=cover.out $(PKG) && $(GO) tool cover -func=cover.out | tail -1

bench:
	$(GO) test -bench=. -benchmem -run='^$$' $(PKG)

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN) cover.out *.ir.bin
	rm -rf dist/

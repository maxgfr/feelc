GO ?= go
PKG := ./...
BIN := feelc

.PHONY: all build test test-race vet fmt cover bench tidy clean run lint-english wasm

all: vet test build

# wasm builds the engine to WebAssembly for the feelc npm package (and the playground).
# Output lands in packages/engine/wasm/ alongside Go's wasm_exec.js runtime glue. wasm_exec.js moved
# from $GOROOT/misc/wasm (Go ≤1.23) to $GOROOT/lib/wasm (Go ≥1.24): try the new path, fall back.
WASM_DIR := packages/engine/wasm
wasm:
	mkdir -p $(WASM_DIR)
	GOOS=js GOARCH=wasm $(GO) build -trimpath -o $(WASM_DIR)/feelc.wasm ./cmd/feelc-wasm
	cp "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" $(WASM_DIR)/wasm_exec.js 2>/dev/null \
		|| cp "$$($(GO) env GOROOT)/misc/wasm/wasm_exec.js" $(WASM_DIR)/wasm_exec.js

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

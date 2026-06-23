# ADR 0017 — In-browser WASM playground

- **Status**: Accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

The strongest way to show that feelc's execution is deterministic and dependency-free is to let people run
the **real** engine with zero setup. A re-implementation in JavaScript would be a second engine to keep in
sync (and could diverge from the Go semantics — defeating the determinism claim). Go compiles to
WebAssembly, so the actual engine can run in the browser.

## Decision

Ship `cmd/feelc-wasm`, a WebAssembly entrypoint that exposes the engine's read-only analysis surface to JS
(`verify`, `run`, `graph`, `trace`, `required`, `check`, `model`) — the same packages the CLI and HTTP
service call. A **build-tag split** keeps the rest of the tree clean: `main_wasm.go` (`//go:build js && wasm`)
holds the browser bindings, `main_stub.go` (`//go:build !(js && wasm)`) keeps `go build ./...` green on
native targets. The network-bound paths are **excluded** from the browser build: no LLM (`internal/genai`)
and no SMT/Z3 (`internal/smt`), since neither is available client-side.

## Consequences

- The playground at <https://maxgfr.github.io/feelc/playground/> runs the identical engine; every result
  matches the CLI bit-for-bit (determinism preserved in the browser).
- LLM *authoring* requires `feelc serve --ui` (a local server proxying your key), not the playground.
- The `.wasm` is built and bundled by `.github/workflows/pages.yml` alongside the docs
  (`GOOS=js GOARCH=wasm go build … ./cmd/feelc-wasm`); see [ADR 0018](0018-docs-site.md).
- Anything verification-wise that needs SMT shows the honest `not-verifiable` fallback in the browser.

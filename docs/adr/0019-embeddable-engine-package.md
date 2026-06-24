# ADR 0019 — Distribute the engine as the `feelc` npm package

- **Status**: Accepted (2026-06-24)
- **Deciders**: maxgfr

## Context

[ADR 0017](0017-wasm-playground.md) compiled the **real** engine to WebAssembly so the playground could
run it client-side. That binding was built for one consumer: it registers a global `feelc` object,
is loaded by a `<script>` tag, takes only `.rules` *source*, and recompiles on every call.

Users want to embed the engine in their **own** TypeScript apps — browser, Node, bundlers
(Vite/webpack/Next), and edge runtimes — with **no HTTP API**, so the engine is portable. The
playground's surface doesn't serve that: it isn't importable, typed, or bundler-friendly, and
recompiling source on every evaluation is wasteful for reactive UIs.

A pure-TypeScript re-implementation was rejected for the same reason as ADR 0017: a second engine to
keep in sync, which would drift from the Go semantics (exact decimals, temporal, three-valued logic)
and defeat the determinism claim.

## Decision

Publish **`feelc`**, a typed, ESM-only npm package that wraps the existing `cmd/feelc-wasm`
build — no second engine.

- **In-repo, single source of truth.** Developed as an npm workspace (`packages/engine`); `make wasm`
  produces the version-matched `feelc.wasm` + `wasm_exec.js`. The TS layer only marshals JSON.
- **Compiled-model handle path.** Extend the WASM surface with `compile`, `load`, `export`,
  `evalCompiled`, `infoCompiled`, `requiredCompiled`, `dispose` (backed by a handle registry), so a
  caller compiles — or loads a precompiled `.ir.bin` — **once** and evaluates **many** times. The
  `.ir.bin` is the canonical artifact from [ADR 0006](0006-ir-serialization.md): byte-identical
  between WASM and the native `feelc compile`, so artifacts are interchangeable across the two tools.
  The original source-based functions are unchanged (the playground still works).
- **Three usage modes:** compile-at-runtime (`run`), compile-once-evaluate-many (`compile` → handle),
  and ship-precompiled (`load`).
- **Isomorphic loader.** `new URL("…", import.meta.url)` for browsers/bundlers, `node:fs` for Node, and
  a `wasmBinary` override for edge (where the `.wasm` is imported as a module). A caller-chosen global
  token (default `feelc`) allows multiple isolated instances per realm.
- **Publishing.** Bootstrap the first release manually (`npm publish`), then CI publishes on a `v*` tag
  via npm **OIDC trusted publishing** — no `NPM_TOKEN` secret (`.github/workflows/npm.yml`).

## Consequences

- **Drift guard (test contract).** `examples/node-smoke` asserts the WASM output equals the native CLI
  for curated cases, and that `.ir.bin` round-trips in both directions across the two tools. Vite's
  build is exercised in CI to keep the bundler asset path working.
- **Decimal precision.** Outputs cross into JS as `number` (float64) via `modelinfo.JSONify`, so very
  large/precise decimals can lose precision in JS. Documented caveat; the engine itself stays exact. A
  `decimalsAsString` mode is a possible future follow-up.
- **Bundle size** ~6 MB (~1.5 MB gzipped), loaded lazily on `createEngine()`. A TinyGo "slim" build is
  **deferred** (TinyGo compatibility with `apd` and the FEEL parser is unverified).
- **ESM-only** (no CJS): keeps `import.meta.url` — used to locate the `.wasm` — clean across all targets.

## Update (2026-06-24)

The package was renamed from `@feelc/engine` to the unscoped **`feelc`**: the `@feelc` npm scope/org did
not exist, so publishing `@feelc/engine` returned `404 — Scope not found`, and an unscoped name needs no
org. The decision (ship the WASM engine as a typed, ESM-only npm package) is unchanged — only the name
and its references were updated (`import … from "feelc"`, the `feelc/wasm/feelc.wasm` subpath, the
`-w feelc` workspace). While there, the `bin` path was normalized to `bin/feelc-compile.mjs` and
`repository.url` to `git+https://…git` so it publishes without warnings.

Publishing also moved: a single release pipeline (`.github/workflows/release.yml`) now owns both the
goreleaser binaries and the gated npm OIDC publish (a tag pushed by `GITHUB_TOKEN` cannot trigger a
separate `npm.yml`); `npm.yml` is now CI-only. See [RELEASING.md](../../RELEASING.md).

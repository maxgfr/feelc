# Architecture

A one-screen map of the engine for contributors. The guiding invariant: **the LLM is out of the core** —
authoring is the only place a model is involved; compile / verify / execute is pure, deterministic Go.

## Compile pipeline (the data flow)

```
.rules source
   │  internal/dsl            parse (cells & expressions via the vendored third_party/feel fork)
   ▼
*model.Model                  AST
   │  internal/compiler       typecheck + lower to bytecode; internal/units dimensional check
   ▼
*ir.CompiledModel  ──────────  canonical, serializable; internal/ir = codec + ir.Hash (the identity)
   │
   ├─ internal/vm (+ engine)   deterministic evaluation (exact decimals via internal/decimal)
   ├─ internal/verify          completeness / conflicts / dead rules / subsumption (geometric)
   │     └─ internal/smt       non-geometric residuals → Z3 (build tag `smt`, ADR 0007)
   ├─ internal/explain         justification traces        internal/graph   decision-requirements graph
   └─ internal/trace           source↔rule mapping         internal/check   NL-claim semantic gate
```

`ir.Hash` is the canonical identity of a compiled model (line numbers are part of it — [ADR 0006](adr/0006-ir-serialization.md));
nothing downstream reorders or renumbers, so a model hashes identically however it is loaded.

## Packages

| Package | Role |
|---|---|
| `internal/dsl`, `internal/model` | Parse `.rules` into the AST (`third_party/feel` handles FEEL cells/expressions). |
| `internal/compiler`, `internal/units` | Typecheck, lower to bytecode, dimensional analysis. |
| `internal/ir` | Canonical `CompiledModel`, the binary codec, and `ir.Hash`. |
| `internal/vm`, `internal/engine`, `internal/decimal` | Deterministic execution with exact decimals. |
| `internal/verify`, `internal/smt` | Formal verification (geometric; SMT/Z3 for the rest). |
| `internal/explain`, `internal/graph`, `internal/trace`, `internal/check`, `internal/modelinfo` | Analysis & introspection surfaces. |
| `internal/loader`, `internal/registry` | Compile-verify-swap pipeline, source-hash cache, file watch, atomic hot-reload. |
| `internal/service` | The HTTP facade ([routes](http-api.md)); `internal/audit` logs every decision. |
| `internal/project` | Multi-module workspaces: namespaced merge, linking, lexical retrieval ([ADR 0015](adr/0015-project-mode.md)). |
| `internal/genai` | The BYO-LLM authoring boundary ([ADR 0008](adr/0008-ai-authoring-layer.md)) — the only LLM caller. |
| `internal/dmnxml`, `internal/tck`, `internal/fmtrules`, `internal/diag` | DMN interop, TCK conformance, formatter, structured errors. |

## Repo layout

| Path | Contents |
|---|---|
| `cmd/feelc` | The CLI ([reference](cli.md)). |
| `cmd/feelc-wasm` | The in-browser engine (`GOOS=js`), via a build-tag stub split ([ADR 0017](adr/0017-wasm-playground.md)). |
| `internal/*` | The engine (above). |
| `third_party/feel` | Vendored FEEL parser fork, pinned via a `replace` in `go.mod` — do not de-vendor. |
| `skill/` | The portable authoring skill (`SKILL.md` + scripts). |
| `site/` | The docs site generator (`build.mjs`) and the WASM playground ([ADR 0018](adr/0018-docs-site.md)). |
| `docs/` | Reference docs + `docs/adr/` (decision records). |
| `examples/`, `sample-project/`, `testdata/` | Verified reference models, a project-mode example, fixtures. |
| `spike/` | Throwaway experiments (separate `go.mod`, not part of `./...`). |

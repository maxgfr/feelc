# ADR 0015 — Project mode (multi-module workspace, namespaced merge, server-side persistence)

- **Status**: accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

Until now a feelc model was a single `.rules` file. Managing several hundred business rules with AI
needs more: organising rules into modules, references across them, server-side persistence (a portfolio,
not a scratch buffer), and project-wide verification. The constraint is to add this **without** touching
the deterministic core (compiler / IR / VM / verify) or breaking the canonical-hash contract (ADR 0006),
and **without** new Go dependencies (single static binary, ADR 0008 ethos).

## Decision

A new package `internal/project` introduces a **project**: a directory of `.rules` modules plus an
optional `feelc.project.json` manifest, compiled and **linked into one `ir.CompiledModel`** that the
existing engine runs unchanged.

**Namespaced merge.** Each module is compiled standalone (reusing `loader.CompileFile`), then every
name is qualified to `module__name` and the modules are concatenated into a single merged model. This is
free because the VM, verifier, DRG and explain resolve decisions by **opaque string name**
(`vm.resolve` → `cm.Decision`): a qualified name is just another string. One project = one merged model
= one project hash (`ir.Hash(merged)`), with **no codec bump**.

**The hash invariant (the load-bearing rule).** Qualification rewrites *only* name strings — the
`Inputs/Domains/InputMeta/Units` keys, `Decision.Name`, `Decision.Deps`, `DecisionTable.Inputs`, and
`ExprProgram.Vars` in `Decision.Expr` and every `CellTest.Prog` (recursing through `Sub`). It never
touches `Line` fields (which ARE hashed) and never qualifies `DecisionTable.Outputs` (context field
labels, not references). A **single-module** project is the identity transform (no prefixing), so a lone
`.rules` served as a project hashes identically to compiling it standalone — the back-compat anchor,
asserted by a test. The merge deep-copies every structure it rewrites, so each module's standalone model
(kept for per-module verify/hashing) is never mutated.

**Cross-module references.** A module references another module's decision through a manifest `uses`
binding: a local **input** alias wired to `othermodule.decision`. The dot lives only in JSON, never in a
FEEL cell (which would mis-parse as a `DotOp`). At link time the alias resolves to the target's qualified
name and the bound input is **omitted** from the merged external inputs (it is satisfied by an upstream
decision). Dangling aliases and cross-module dependency cycles are rejected at load.

**Persistence + golden rule.** A `Workspace` makes the directory mutable: `PUT/POST/DELETE` on
`/v1/modules` and a directory watcher (fsnotify, mirroring `loader.Watch`). Every mutation validates an
in-memory candidate first (`project.Compile`), then writes the module file before the manifest (atomic
temp+rename, with rollback) and swaps the registry only if it links — an invalid edit keeps the current
project. Publishing is serialized (one `publishMu`) so the registry model and project snapshot swap
together. `Workspace` stays pure and unit-tested.

**Safe-by-default hardening.** The write endpoints are gated behind an explicit `--allow-edit` flag (off
by default; without it `PUT/POST/DELETE /v1/modules` 404 and the watcher/reload still work). Module names
use a strict allowlist (`^[A-Za-z][A-Za-z0-9_]*$`), manifest module paths are rejected if they escape the
project directory (no `..`/absolute traversal), module count is capped, every request body is size-limited
(`http.MaxBytesReader` + an explicit `http.Server` with read/idle timeouts), and the Docker image runs as a
distroless **nonroot** user with a read-only default command. The service is unauthenticated by design, so
`--ui`/`--allow-edit` remain trusted/loopback-host tools (ADR 0008 ethos).

**Surface.** `feelc serve --project <dir>` (mutually exclusive with `--rules`); read endpoints
`/v1/project`, `/v1/modules`, `/v1/modules/{name}/source`, `/v1/project/health|graph`, candidate
`POST /v1/project/verify`; mutating `PUT/POST/DELETE /v1/modules/...` (loopback-only CORS). The embedded
UI feature-detects project mode via `GET /v1/project` (404 in single-file mode) and additively injects a
module navigator, per-module editor + Save, a health dashboard, and the cross-module graph — the
single-file UI and the WASM playground are unchanged.

## Consequences

- The engine packages are untouched; the whole feature lives in `internal/project`, additive service
  handlers, and additive embedded JS/CSS. No new Go module dependency; CGO-free single binary preserved,
  now shippable as a Docker service (`Dockerfile`, `--project /work` volume).
- Project health reuses `verify.Verify` per module; `Module.Hash` is the hook for incremental
  re-verification (a later optimisation slice). Cross-module same-named inputs surface as an advisory.
- Determinism/auditability are unchanged: the merged model is an ordinary `ir.CompiledModel`, reproducibly
  hashed and served by the same deterministic VM.
- Deferred: AI authoring scoped to a project with lexical retrieval (next phase); per-module incremental
  verification cache and decision-table indexing (optimisation phase). `effectiveDate` is parsed but not
  yet used (as-of evaluation, roadmap slice 7).

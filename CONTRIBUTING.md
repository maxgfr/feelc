# Contributing to feelc

feelc is a business rules engine (DMN/FEEL) compiled to a **deterministic** engine in Go.
Guiding principle: **never silently conform/pretend** — anything out of scope
fails plainly (or is reported as honest degradation), never accepted-then-misinterpreted.

## Prerequisites & build

- **Go 1.23+** (see `go.mod` and the CI/release workflows).
- The FEEL parser is a **vendored fork** under `third_party/feel`, pinned via a `replace` in
  `go.mod` (exports `FunCall.Args`, fixes a parser DoS — see [ADR 0004 §1](docs/adr/0004-deferrals.md)).
  Do not "de-vendor" it.

```sh
go build ./...
go vet ./...
go test -race ./...                 # everything must be green
go test -tags smt ./internal/...    # optional SMT backend (see ADR 0007; requires z3 for a proof)
```

The SMT job needs `z3` on `PATH` (`brew install z3` / `apt-get install z3`); it is **not** gated in CI,
so verify it locally when touching `internal/smt` or `internal/verify`. The `spike/` submodule (throwaway)
has its own `go.mod`; it is not part of `./...`.

## Discipline

- **TDD**: red test first, then the minimum to go green; refactor while green.
- **`go test -race ./...` + `go vet` green** before any commit.
- **Determinism**: no source of nondeterminism in the decision path. The **goldens**
  (`internal/engine/golden_test.go`) are replayed in CI on **amd64 + arm64** (bit-for-bit proof).
  Regeneration: `FEELC_REGEN_GOLDEN=1 go test ./internal/engine -run Golden`.
- **Pivots** (changes to serialize, never parallelize): `internal/ir/match.go` (single source
  VM+verify), `internal/compiler/lower_expr.go` (lowering extension point), the
  `internal/ir/codec.go` codec (any struct change alters `ir.Hash` → regenerate the goldens).

## Commits & release

- **Conventional Commits** (`feat:`, `fix:`, `ci:`, `docs:`, `test:`…): consumed by
  semantic-release. A `feat:`/`fix:` pushed to `main` triggers a release — goreleaser publishes the
  multi-OS/arch binaries to the GitHub Release, and (once npm OIDC is bootstrapped) `feelc` is
  published to npm. Non-release commits (`ci:`, `docs:`, `test:`…) do not publish. The full pipeline and
  the one-time npm bootstrap are documented in [RELEASING.md](RELEASING.md).
- End messages with: `Co-Authored-By: ...` if relevant.

## ADR

Architecture decisions live in [`docs/adr/`](docs/adr/) — see
[`docs/adr/README.md`](docs/adr/README.md) for the template, the index, and the full lifecycle. In short:
any **structuring** decision (a new core capability, a dependency, a language/format/policy choice, or a
**deferral**) gets an ADR. Copy [`0000-template.md`](docs/adr/0000-template.md), number it highest+1
(numbers are never reused or renumbered). ADRs are **append-only**: an accepted `## Decision` is never
rewritten — a non-reversing follow-up is a dated `## Update`, a reversal is a new *superseding* ADR (flip
the old one's Status to `Superseded by ADR NNNN`). A deferral must be documented, not hidden.

## Docs & site

The published site (<https://maxgfr.github.io/feelc/>) is generated from this repo by
[`site/build.mjs`](site/build.mjs) (deployed by `.github/workflows/pages.yml`). `docs/*.md` is the single
source of the reference pages (registered in the `DOCS` array) and `docs/adr/*.md` is auto-discovered and
published behind the **Decision records** index. Build/preview locally:

```sh
npm i --no-save marked && node site/build.mjs    # renders site/docs/ + site/examples.json
# WASM playground (optional): GOOS=js GOARCH=wasm go build -o site/static/feelc.wasm ./cmd/feelc-wasm
```

Editing a `docs/*.md` page or adding a `docs/adr/NNNN-*.md` is all that is needed — the nav updates
automatically. See [`docs/architecture.md`](docs/architecture.md) for the package map and repo layout.

## The authoring skill (`skill/`)

The `feelc-rules` skill lives in the **`skill/` subdirectory** of the repo (not at the root). A
bare `npx skills add maxgfr/feelc` is therefore **NOT enough**: it targets the root, which has no
`SKILL.md`. Use the **tree-URL** pointing at the subdirectory:

```sh
npx skills add https://github.com/maxgfr/feelc/tree/main/skill
```

The skill never decides a result "off the top of its head": the `feelc` binary (compile / verify / run /
check / explain) is the **deterministic oracle**.

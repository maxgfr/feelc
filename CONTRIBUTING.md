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

The `spike/` submodule (Slice 0, throwaway) has its own `go.mod`; it is not part of `./...`.

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
  semantic-release. A `feat:`/`fix:` pushed to `main` triggers a release (goreleaser publishes the
  multi-OS/arch binaries). Non-release commits (`ci:`, `docs:`, `test:`…) do not publish.
- End messages with: `Co-Authored-By: ...` if relevant.

## ADR

Architecture decisions live in `docs/adr/` (numbering: 0001 FEEL frontend, 0002
decimal, 0003 null/error, 0004 deferrals, 0005 structured errors, 0006 IR serialization, 0007
SMT backend). Any structuring decision adds/updates an ADR (the project's ethics
require it: a deferral must be documented, not hidden).

## The authoring skill (`skill/`)

The `feelc-rules` skill lives in the **`skill/` subdirectory** of the repo (not at the root). A
bare `npx skills add maxgfr/feelc` is therefore **NOT enough**: it targets the root, which has no
`SKILL.md`. Use the **tree-URL** pointing at the subdirectory:

```sh
npx skills add https://github.com/maxgfr/feelc/tree/main/skill
```

The skill never decides a result "off the top of its head": the `feelc` binary (compile / verify / run /
check / explain) is the **deterministic oracle**.

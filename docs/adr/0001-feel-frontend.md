# ADR 0001 — FEEL front-end: parsing dependency vs. in-house parser

- **Status**: accepted (Slice 0, 2026-06-22)
- **Deciders**: maxgfr
- **Technical context**: feelc needs to parse two things — the **table cells** (unary
  tests: `< 580`, `[580..680)`, `"a","b"`, `not(...)`, `-`) and the **expressions** of the
  literal-expression decisions (`monthly_debt / (annual_income / 12)`).

## Context

The plan considered an **in-house FEEL parser** (Pratt + dual root, ~1500-2500 lines) as the likely
default path, with the adversarial review judging the `pbinitiative/feel` candidate insufficient (based on a
stale version: "7 stars, no unary tests"). The gate rule: **measure before planning**.

## Empirical evaluation (spike `spike/`)

`pbinitiative/feel@v1.0.6` (MIT) was evaluated against 20 unary-tests + 10 expressions representative
of the 4 examples (see `spike/main.go`). Result: **17/20 unary-tests, 10/10 expressions**.

Key findings:
- `Parse()` / `ParseString` parse **in unary-test context by default**: `< 580` yields
  `Binop{Left: Var{"?"}, Op:"<", Right: 580}` — exactly the "implicit comparison against the column
  value" semantics we need. A normal expression falls back to `p.expression()`.
- **Exported and usable AST**: `Binop`, `RangeNode{StartOpen,Start,EndOpen,End}`, `MultiTests`,
  `NumberNode{Value string}` (**source literal preserved** → re-parsable exactly with apd),
  `StringNode`, `Var`, `FunCall`, `IfExpr`, `BoolNode`, all carrying a `TextRange` (source positions).
- The 3 failures are **non-blocking**: `-` (any/don't-care) is a **DSL-level** marker, handled
  before the parser; `]0..100]` is an alternative notation we do not allow (the standard form
  `(0..100]`/`[0..100)` works); `not(< 18)` (negated unary-test with operator) is a narrow gap to
  handle in our cell normalizer or to defer.
- **Assumed limitation**: their `Number` type is a `big.Float` (binary, 272 bits), **not exact decimal**,
  and their interpreter is a tree-walker. Neither is suitable for feelc's determinism.

## Decision

1. **Use `github.com/pbinitiative/feel` as a dependency** for the **lexer + parser + AST** of
   expressions and unary-tests. No initial fork: the public API (`ParseString`) is sufficient and the license
   is MIT.
2. **Write ourselves** the typecheck, the lowering to the IR, and the VM. **Do NOT use** their
   interpreter (`EvalString`) nor their `Number` (`big.Float`).
3. Exact decimal goes through **apd** (cf. [ADR 0002](./0002-decimal.md)) by re-parsing
   `NumberNode.Value` (the source literal).
4. Handle `-` (any) and, if needed, `not(<test>)` in **our DSL/cell-normalization layer**.
5. **Reassess fork/vendoring** only if we have to fix the 2 gaps (`not(<test>)`, range
   notations) or if an upstream drift appears. Pin the exact version in `go.mod`.

## Consequences

- **Major gain**: ~2000 lines of in-house parser avoided ⇒ Slices 1-2 strongly de-risked.
- **External dependency** in the core (vs. the ideal "zero-dep") — acceptable: MIT, small, stable AST.
  Mitigation: the typecheck is our **scope gatekeeper** (rejects any construct outside the subset),
  and a subset-coverage test protects against upstream regressions.
- **Determinism preserved**: we inherit no float; parsing is pure (the literal stays a string).
- Spike kept under `spike/` (separate module, excluded from the main build) as **reproducible proof**.

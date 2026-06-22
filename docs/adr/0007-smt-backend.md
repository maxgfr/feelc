# ADR 0007 — Optional SMT (Z3) backend, behind a build tag

- **Status**: accepted (2026-06-22)
- **Deciders**: maxgfr

## Context

feelc's verification relies on a **hyper-rectangle algebra** (geometric decomposition
into atomic cells). It covers tables whose cells are all normalized `CellTest`s.
`Op=Prog` cells (free expression: cross-column arithmetic, reference to
another column) are **not** geometrically decidable — they are reported as
**honest degradation** `not-verifiable` (ADR 0004 §2). An SMT solver (Z3) can decide
completeness/conflicts on these residuals.

## Decision

**Optional** SMT backend, **isolated behind the `smt` build tag** — off the critical path, zero
dependency (neither CGo nor binary) in the default build.

- **Extension point**: `var smtProve func(cm, d, rep) bool` in `internal/verify/verify.go`.
  nil by default → unchanged behavior (`not-verifiable` on `Op=Prog`). A backend returns
  `true` if it handled the decision.
- **PURE and testable encoder**: `internal/smt` translates the geometric layer (`CellTest`) and the
  straight-line bytecode (`ExprProgram`) into SMT-LIB2 (Reals + Bools theory). **Without external
  dependency → unit-tested without Z3.** Subset: arithmetic, comparisons, and/or/not,
  intervals, sets, negation, number/boolean columns. Outside the subset (if/then/else
  compiled to jumps, floor/ceiling/round, string columns, decision dependencies) → cleanly
  refused (`ok=false`).
- **Z3 wiring**: `internal/verify/verify_smt.go` (`//go:build smt`) plugs in `smtProve`, encodes
  a completeness query (`unsat` ⇒ complete table), and invokes `z3 -in`.

**HONEST degradation (never silently conform)**: z3 missing from PATH, or form outside the
encodable subset → `not-verifiable` with the reason; never a false proof.

## Consequences

- Default build: no impact (nil extension point), reproducible and dependency-free.
- `-tags smt` build: `go build -tags smt`, requires `z3` in PATH for an effective proof
  (otherwise explicit `not-verifiable`). Reflects the final opcodes (post-Slice 22: if/built-ins).
- The encoder is tested; the effective Z3 proof path is validated where `z3` is installed.
- **Follow-up**: extend the encoder (if/then/else via `ite`, `floor/ceiling/round` via `to_int`),
  and also route conflict detection to SMT.

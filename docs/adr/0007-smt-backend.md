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
  bytecode (`ExprProgram`) into SMT-LIB2 (Reals + Bools + Ints theory). **Without external
  dependency → unit-tested without Z3.** Subset: arithmetic +-*/ and unary negation, comparisons,
  and/or/not, intervals, sets, negation; **if/then/else** (the strictly-nested `OpJmpFalse`/`OpJmp`
  backpatch is reconstructed into `ite`); **floor/ceiling** (via `to_int`); **round** (HALF_EVEN, via
  a fresh `Int` with parity constraints — needs an `Aux` sink); number/boolean columns. Outside the
  subset (string columns, decision dependencies, an `Aux`-less round) → cleanly refused (`ok=false`).
- **Z3 wiring**: `internal/verify/verify_smt.go` (`//go:build smt`) plugs in `smtProve`, encodes a
  **completeness** query (`unsat` ⇒ complete table) **and a conflict** query (`sat` ⇒ overlapping
  rules — UNIQUE: any overlap; ANY: divergent outputs, mirroring the geometric `recordConflict`),
  and invokes `z3 -in`. Logic `ALL` (mixed Real/Int) for the `to_int`/`mod` terms.

**HONEST degradation (never silently conform)**: z3 missing from PATH, or form outside the
encodable subset → `not-verifiable` with the reason; never a false proof.

## Consequences

- Default build: no impact (nil extension point), reproducible and dependency-free.
- `-tags smt` build: `go build -tags smt`, requires `z3` in PATH for an effective proof
  (otherwise explicit `not-verifiable`). Reflects the final opcodes (post-Slice 22: if/built-ins).
- The encoder is tested; the effective Z3 proof path is validated where `z3` is installed.
- **Follow-up (done, 2026-06-22)**: the encoder now handles **if/then/else** (`ite`), **floor/ceiling**
  (`to_int`) and **round** (HALF_EVEN, exact — soundness pinned by `TestRoundSoundnessZ3`), and
  conflict detection is routed to SMT (UNIQUE/ANY). if/then/else is now also accepted in cells
  (compiler routes `IfExpr` → `Op=Prog`). See `examples/smt-residual/` for an end-to-end
  `verify --tags smt` demo. Remaining: a `mod`-based encoding makes a non-half-integer `round`
  domain larger; multi-arg built-ins stay deferred (ADR 0004 §3).

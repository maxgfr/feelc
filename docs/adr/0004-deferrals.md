# ADR 0004 — Acknowledged deferrals: parameterized BKM and SMT extension

- **Status**: accepted (2026-06-22); temporal deferral since lifted by [ADR 0014](0014-temporal-types.md); §3 multi-arg builtin deferral partially lifted by [ADR 0020](0020-deterministic-extra-builtins.md) (2026-06-24)
- **Deciders**: maxgfr

In line with the project's ethics ("never silently conform/pretend"), we explicitly document
two features of the "complete" scope that are **deferred**, along with their rationale.

## 1. BKM / Parameterized invocation (Slice 7) — ✅ LIFTED (2026-06-22, Slice 13)

> **Status updated: deferral LIFTED.** Parameterized BKM is implemented. We **forked** and vendored
> `pbinitiative/feel` under `third_party/feel` (pinned via `replace`) to **export `FunCall.Args`**
> (`[]FunCallArg{Name, Arg}`), which unblocks reading the arguments. The syntax is
> `bkm name(p:t, …):ret = expr` (signature parsed on the DSL side, not in the fork); the invocation
> `name(a, b)` is **inlined at compile time** through AST substitution of the parameters
> (`internal/compiler/lower_expr.go`) — **zero new opcode**, the VM/IR unchanged. Recursion
> (self/mutual) is detected statically and **rejected outright**; depth and instruction-budget
> guardrails (bounded RAM). The fork also fixes an **upstream DoS** (infinite parser loop on an
> explicit `?`). The historical observation below is kept for the record.

**Technical observation (historical).** feelc reuses `github.com/pbinitiative/feel` as the FEEL parser
(ADR 0001). However, its AST node `FunCall` exposes `Args []funcallArg` where **`funcallArg` is an
unexported type** whose fields (`argName`, `arg`) are themselves unexported. It is therefore
impossible, via the public API, to **read the arguments of a function call** `name(arg1, arg2)`.
Implementing a parameterized BKM invocation would require **forking** the parser (which ADR 0001
anticipated as a fallback).

**Decision.** Defer parameterized BKM. The cost/value tradeoff is unfavorable for now:
- **None of the 4 reference examples** need it (the adversarial review had pointed this out).
- **Non-parameterized reuse** is already covered: a literal-expression decision
  (`decision x : number = …`) is a reusable named expression, referenced by its name in
  other decisions' `needs:` (DRG).

**Resumption.** Fork `pbinitiative/feel` to export `funcallArg` (or write the in-house FEEL parser
planned as a fallback by ADR 0001), then inline invocations at compile time (no call frame).

## 2. SMT extension (Z3) — deferred (optional, behind build tag)

Formal verification (Slice 4) relies on a **hyper-rectangle algebra**: it covers tables whose
cells are all normalized `CellTest` (comparisons, intervals, sets). Cells with `Op=Prog`
(reference to another column, inter-column arithmetic) are **not** geometrically decidable and
are already reported as **honest degradation** (`not-verifiable`).

**Decision.** The SMT extension (Z3) for proving properties on these non-rectangular cells
remains **optional and deferred**, behind a build tag, off the critical path. The geometry
covers the bulk of DMN tables without an external dependency (neither CGo nor a Z3 binary).

**Resumption.** When a real need for provable inter-column conditions arises: integrate Z3
(via binary or binding) under `//go:build smt`, and route `Op=Prog` cells to the solver.

## 3. Multi-argument FEEL built-ins — deferred (Slice 22)

Slice 22 adds the **pure single-arg** built-ins `floor` / `ceiling` / `round(x)` (+ `not`),
`if/then/else`, and geometric `not(<test>)`. The **multi-argument** built-ins remain **out of
scope** and **fail outright** (never silently conformed):

- `round(x, n)` (rounding to `n` decimals),
- `substring(s, start[, len])`, and the other multi-arg string/list built-ins.

**Rationale.** The v2 scope deliberately stays minimal (decidable, deterministic subset);
these functions would add semantics (decimal handling, string indices) with no justifying
example. The lowerer emits an explicit diagnostic referring to this ADR. **Resumption.** Add
the dedicated opcode/lowering + tests when a real need arises.

> **Status updated (2026-06-24): partially LIFTED by [ADR 0020](0020-deterministic-extra-builtins.md).**
> `round(x, n)`, `abs(x)`, `trunc(x)` and `modulo(x, y)` are now supported (deterministic, exact,
> verification-safe — a gap analysis vs other rule engines showed they were excluded only by this
> blanket rule, not by philosophy). Genuinely problematic multi-arg built-ins (`substring`, string/list
> functions, `for`/`some`/`every`) remain out of scope and still fail outright.

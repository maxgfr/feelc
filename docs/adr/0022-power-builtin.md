# ADR 0022 — `power(x, n)` built-in (integer-exponent, exact)

- **Status**: Accepted (2026-06-24)
- **Deciders**: maxgfr

## Context

Exponentiation is the one common arithmetic primitive feelc lacked. It appears constantly in real
rules: compound interest / growth `principal * power(1 + rate, years)`, squared tolerances, area /
volume factors, geometric schedules. Two obstacles:

1. The `**` (and `^`) operators are **not lexed** by the vendored FEEL scanner (`third_party/feel`),
   and feelc does not modify the vendored parser (ADR 0001). So an operator form is off the table.
2. The obvious library call `apd.Pow` is **transcendental** (exp/ln based) and therefore **inexact
   even for integer exponents** — incompatible with feelc's exact-decimal / bit-for-bit-determinism
   contract (ADR 0002).

## Decision

Add a **`power(x, n)` function-form built-in** — the same discipline ADR 0020 used for `modulo` (a
FEEL-standard function, parser untouched, `%` stays the percent literal).

- **Integer, non-negative exponent only.** `n` must be a whole number with `0 ≤ n ≤ 1000`;
  non-integer, negative, or out-of-range `n` is a **loud runtime error** (negative powers produce
  non-terminating fractions, e.g. `power(3, -1)` → 0.333…, which is not exact).
- **Exact via repeated multiplication** on the frozen Decimal128 context — `power(x, n)` is
  bit-identical to writing `x * x * … * x` (n factors). This guarantees consistency with the `*`
  operator and cross-platform determinism. Never `apd.Pow`.
- New opcode `OpPow` (`internal/ir/expr.go`), lowered from the `power` invocation
  (`internal/compiler/lower_expr.go`) and executed in `binaryNum` (`internal/vm/expr.go`). Handled in
  **all four** opcode walkers: VM execute, SMT (`internal/smt` — `default` ⇒ not-encodable ⇒ the
  table degrades to *not-verifiable*, sound), `maxStack`, and the units checker
  (`internal/compiler/units_check.go`).
- **Units**: the static model cannot represent `Uⁿ` for a runtime `n`, so the base must be
  **dimensionless** (a dimensioned base is a loud unit error — never a silent unit drop). `null` /
  non-applicable propagate as for the other numeric built-ins.

## Consequences

- Closes the last common arithmetic gap; growth/interest/area rules are now first-class and exact.
- The `**` / `^` operators remain rejected (guardian-of-scope tripwire `a ** 2` stays red); the
  supported surface is exactly `power(x, n)`.
- Tables that route a `power(…)` cell through `Op=Prog` degrade to *not-verifiable* honestly (the SMT
  layer does not encode `OpPow`), never falsely claiming completeness.
- Tests: `internal/engine/feel_ext_test.go` (`TestPowerBuiltin`) incl. the `power(x,4) == x*x*x*x`
  consistency check and the non-integer / negative error guards.

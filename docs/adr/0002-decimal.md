# ADR 0002 — Decimal arithmetic: cockroachdb/apd vs in-house int128

- **Status**: accepted (Tranche 0, 2026-06-22)
- **Deciders**: maxgfr

## Context

feelc MUST be **bit-for-bit deterministic across platforms**: this is the product's central thesis
(reproducible, auditable, replayable decisions). The credit/insurance domains handle amounts
and rates (`dti = monthly_debt / (annual_income / 12)`, compared against `0.43`) where binary arithmetic
(`float64`, `big.Float`) introduces representation errors (`0.1 + 0.2 != 0.3`) and uncontrolled
rounding. We therefore need an **exact decimal** with a fixed **HALF_EVEN** (banker's rounding) mode.

The "complete" plan mentioned an **in-house int128 decimal** (int128 mantissa + scale). The adversarial
review classified this as a **multi-week trap** (Go has no native int128; correct add/sub/mul/div +
rounding + parsing/formatting = a project in itself, a source of subtle determinism bugs).

## Empirical evaluation (spike `spike/decimal_test.go`)

`github.com/cockroachdb/apd/v3@v3.2.3` (Apache-2.0, used in production by CockroachDB) verified:
- **Exactness**: `0.1 + 0.2 == 0.3` exact; `1500 / (60000/12) == 0.3` exact (credit dti case). ✅
- **HALF_EVEN**: `2.5→2`, `3.5→4`, `2.125→2.12`, `2.135→2.14` (at 2 decimals). ✅

## Decision

1. **Use `cockroachdb/apd/v3`** as feelc's decimal engine from v1. Fixed context:
   sufficient precision (≥ 34 digits, Decimal128 type) and `Rounding = RoundHalfEven` by default,
   the model's rounding (`rounding: half_even`) being stored in the IR.
2. The wrapper lives in `internal/decimal`: it exposes only the strict necessities (parse from the
   source literal, +, -, *, /, comparison, quantize/rounding) and **freezes the context** to guarantee
   determinism — no dependency on mutable global state.
3. The **in-house inline int128 remains a deferred micro-optimization**, to be considered only **after**
   benchmarks (`testing.B -benchmem`) show that apd is the bottleneck of the hot path. Not a v1 architecture decision.

## Consequences

- Determinism and exactness **acquired immediately**, without weeks of fragile numerical code.
- One more Apache-2.0 dependency (compatible with feelc's Apache-2.0 license).
- The VM's `Value` will carry an apd decimal (or a compact view of it); the allocation cost
  of apd in the hot path will be **measured** in Tranche 4/5 and optimized if necessary (pooling, or int128).

# Example — progressive brackets (`bracket:`)

Marginal-rate income tax: each tranche taxes only the portion of income inside it. The `bracket:`
mechanism (Publicodes' `bareme`) is **lowered to ordinary arithmetic bytecode** at compile time —
there is no special runtime op — so it stays exact and deterministic.

- Rates may be **percent literals** (`11%` == the exact decimal `0.11`).
- Tranches are `[lo..hi)` ranges; the top one is open-ended (`>= lo`).

## Try it

```sh
feelc run --rules examples/income-tax/tax.rules --decision tax --input '{"taxable":30000}'
# 2286.23   (17503 × 11% + 1203 × 30%)
feelc run --rules examples/income-tax/tax.rules --decision tax --input '{"taxable":5000}'
# 0
```

This decision is an expression (the lowered formula), so `verify` reports it as evaluated rather than
geometrically proven; correctness is anchored by unit tests (`internal/engine/bracket_test.go`).

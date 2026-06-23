# ADR 0012 — Physical units & money (compile-time dimensional analysis)

- **Status**: accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

Publicodes and Catala track physical units / money and reject dimensionally-inconsistent arithmetic
(the classic "€ + €/meal" mistake). feelc computed with bare exact decimals, so a unit error slipped
through silently. We want units **without** touching the deterministic VM.

## Decision

Units are a **compile-time, type-level** concern; runtime values stay plain exact decimals (zero VM
impact). An input may declare a unit:

```
input salary : number >= 0 unit "EUR/month"
input rent   : number >= 0 unit "EUR"
decision savings : number = salary - rent   # COMPILE ERROR: EUR/month vs EUR
```

- `internal/units` is a tiny dimensional algebra: a unit is a multiset of base symbols with integer
  exponents, parsed from `"EUR/month"`, `"kg.m/s^2"`, etc.; supports Mul/Div/Equal and a canonical
  String.
- The compiler infers a unit for every literal-expression decision by walking its bytecode (the same
  structured decode as the SMT encoder, so `if/then/else` and brackets are handled): `+`/`-` and
  comparisons require equal dimensions; `*`/`/` combine; `floor/ceiling/round/neg` preserve. Table
  decisions are dimensionless (outputs are literals). Inferred units are recorded in `cm.Units` and
  surfaced in `feelc run` (`1950 EUR`), `/v1/model`, the graph and docs.
- **Leniency toward dimensionless**: a dimensionless operand (numeric constants, the `0` in `if`/
  bracket branches) is unit-neutral in `+`/`-`/comparisons/`ite` — so `fee + 10` and the lowered
  bracket formula type-check, while genuine mismatches (`EUR + month`) are still rejected.
- **Money** is modelled as `number unit "<currency>"` (exact-decimal already gives cent precision);
  no separate `money` type. Units are descriptive: **not in the canonical hash/codec** (like ADR 0010
  metadata), so two models differing only by units share computational identity.

## Consequences

- Catches dimensional bugs at compile time with a precise message + line; zero runtime cost; the VM,
  hash and `.ir.bin` are unchanged.
- Scope (v1): units are checked on literal-expression decisions; table cells are not unit-checked
  (they are predicates). Percent literals (ADR 0011) are dimensionless ratios.

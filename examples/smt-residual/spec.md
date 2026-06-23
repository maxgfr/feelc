# Example — SMT residual (Op=Prog cells, ADR 0007)

This example exists to exercise the **optional SMT (Z3) backend** (ADR 0007). Its cells are
**non-geometric** (`Op=Prog`): they compare the table column `?` against *another input*
(`threshold`) and apply `floor`. The hyper-rectangle algebra used by `feelc verify` cannot
decompose cross-column cells, so by default they are reported as an honest `not-verifiable`
residual. Built with `-tags smt` and with `z3` in `PATH`, the residual is discharged by the solver.

## Decisions

- **`band`** (`string`, hit `first`): `exceeds` if `floor(amount) >= threshold`, else `at_threshold`
  if `amount >= threshold`, else `under`. Proven **complete** (the `-` default closes any gap).
- **`side`** (`string`, hit `unique`): `below` if `amount < threshold`, else `at_or_above`. The two
  rules tile the domain, so it is proven **complete** *and* **conflict-free** (no overlap).

## Run

```sh
feelc run --rules examples/smt-residual/risk.rules --decision band --input '{"amount":5,"threshold":3}'
# "exceeds"   (floor(5)=5 >= 3)
feelc run --rules examples/smt-residual/risk.rules --decision side --input '{"amount":3,"threshold":3}'
# "at_or_above"
```

## Verify

```sh
# Default build: honest degradation (the residual is not geometrically decidable).
feelc verify --rules examples/smt-residual/risk.rules
#   [warning] band — table not provable geometrically (expression cell Op=Prog) — residue not verified

# With the SMT backend (requires z3 in PATH): the residual is discharged by Z3.
go build -tags smt -o feelc ./cmd/feelc
feelc verify --rules examples/smt-residual/risk.rules
#   [info] band — completeness PROVEN by SMT (no gap) — residual cleared
#   [info] side — completeness PROVEN by SMT (no gap) — residual cleared
#   [info] side — consistency PROVEN by SMT (no conflict) — residual cleared
```

> Determinism note: the SMT path is **optional and off the default critical path** (zero
> dependency without the build tag). When `z3` is absent it degrades to `not-verifiable` — never a
> false proof.

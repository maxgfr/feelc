# Templates — the 4 reference models

The `feelc` repository provides 4 complete examples in `examples/` (each with a `spec.md`). Reuse them
as patterns. Summary of the techniques they illustrate:

## Credit (`examples/credit`) — FIRST + DRG + context
Intermediate decision `dti` (expression), final decision `eligibility` (FIRST table, ranges +
comparisons, context output `{eligible, reason}`, `default` row). FIRST = priority rejections first.

```
decision dti : number = monthly_debt / (annual_income / 12)
decision eligibility : Eligibility {
  needs: credit_score, dti, age
  hit: first
     < 580      | -       | -     => false | "insufficient score"
     [580..680) | <= 0.43 | >= 18 => true  | "approved with conditions"
     >= 680     | <= 0.43 | >= 18 => true  | "approved"
     default    |         |       => false | "not covered"
}
```

## Insurance (`examples/insurance`) — COLLECT sum + DRG
**Cumulative** risk surcharges (`hit: collect sum`), then `premium = base_premium + surcharge`.

## Benefits (`examples/benefits`) — COLLECT (list) + boolean
Cumulative benefits → `hit: collect` returns the **list** of granted benefits; boolean condition `true`.

## Promos (`examples/promo`) — COLLECT max
Several applicable discounts, keep the largest → `hit: collect max`.

## Cross-cutting advice
- Bounded domains on numeric inputs → verifiable completeness.
- One final decision per "business question"; factor calculations into intermediate decisions.
- Test each example: `run --decision <name> --input '{…}'` then `verify`.

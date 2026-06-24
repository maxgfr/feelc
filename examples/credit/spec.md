# Reference example — Credit eligibility / scoring

The canonical example of a BRMS (the "hello world" of IBM ODM). It exercises the key building blocks of feelc:
**linked** decisions (DRG), intermediate **FEEL expression**, **table** with ranges and comparisons,
**FIRST** hit policy, **default** row, and multi-field **context** output.

## Inputs (Input Data)

| Name            | Type   | Domain         |
|-----------------|--------|----------------|
| `credit_score`  | number | `[300..850]`   |
| `annual_income` | number | `>= 0`         |
| `monthly_debt`  | number | `>= 0`         |
| `age`           | number | `[0..120]`     |

## Decisions

1. **`dti`** (number) — monthly debt-to-income ratio: `monthly_debt / (annual_income / 12)`. The
   division is guarded so the model stays **total** over the declared domain: `annual_income = 0`
   (allowed by `>= 0`) yields `1` when there is debt (unserviceable → rejected as "debt too high")
   and `0` otherwise, instead of dividing by zero.
2. **`eligibility`** (context `{eligible, reason}`) — FIRST table on `credit_score`, `dti`, `age`.

## Business rules (order = priority)

1. score `< 580` → denial "insufficient score"
2. `dti > 0.43` → denial "debt level too high"
3. `age < 18` → denial "minor"
4. score `[580..680)` and `dti <= 0.43` and `age >= 18` → approval "approved with conditions"
5. score `>= 680` and `dti <= 0.43` and `age >= 18` → approval "approved"
6. otherwise (`default`) → denial "not covered"

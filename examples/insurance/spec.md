# Reference example — Insurance pricing

Exercises **COLLECT C+ (sum)** of cumulative risk elements and a **DRG** (the premium depends
on the computed surcharge).

## Inputs
- `age` (number, `[18..100]`), `region` (string, `{urban, suburban, rural}`),
  `claims` (number, `>= 0`), `base_premium` (number, `>= 0`).

## Decisions
1. **`surcharge`** (number, `collect sum`) — sum of the triggered surcharges:
   - age `[18..25)` → +300 ; region `urban` → +150 ; `claims >= 3` → +500 ; age `>= 70` → +200.
2. **`premium`** (number) — `base_premium + surcharge`.

## Examples
- age 22 / urban / 4 claims / base 1000 → surcharge 950 → **premium 1950**.
- age 40 / rural / 0 claims / base 800 → surcharge 0 → **premium 800**.
- age 72 / urban / 0 claims / base 1000 → surcharge 350 → **premium 1350**.

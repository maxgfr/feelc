# Writing a feelc model — the interview

Don't guess the business logic: elicit it. Ask these questions (group them, but cover everything):

## 1. The inputs (Input Data)
- What data feeds the decision? For each one: **name**, **type** (number/string/boolean),
  and above all its **domain** (`number in [0..120]`, `>= 0`, `string in {"urban","rural"}`).
  → Domains make **completeness verifiable**: declare them as early as possible.

## 2. The decisions and their graph (DRG)
- What is/are the **final decision(s)**? What **intermediate decisions** (e.g. a ratio, a
  score)? A decision can depend on another via `needs:` → feelc evaluates it on demand.
- A computed intermediate decision = **literal-expression**: `decision dti : number = a / b`.
- A case-based decision = **table**.

## 3. For each table: the hit policy
- Are the cases **mutually exclusive**? → `unique` (and `verify` will prove exclusivity).
- Do you want the rows' **priority order**? → `first` (priority rejections first, for instance).
- Several **cumulative** effects? → `collect` (list) or `collect sum` (sum).
- The **best** value? → `collect max` / `collect min`.
- See `references/feel.md` for the complete list.

## 4. The output
- A single value → scalar type (`: number` / `: string` / `: boolean`).
- Several fields (e.g. `{eligible, reason}`) → declare a `type … = context { … }`.

## 5. The edge cases
- What happens at the **bounds** (equality, domain min/max)? Encode them explicitly.
- Is there a **default** case? For a single-hit table, add a `default` row if not all
  cases are covered (otherwise `verify` will flag a gap — which is often the sign
  that a rule is missing).

## Starting skeleton

```
model "<domaine>" {}

input ... : ...
type Resultat = context { ... }      # si sortie multi-champs

decision <intermediaire> : number = <expr>     # si besoin

decision <final> : Resultat {
  needs: ...
  hit: first
  ... | ... => ... | ...
  default | => ...
}
```

Then: `verify` (deterministic gate) → `run` on the edge cases → iterate.

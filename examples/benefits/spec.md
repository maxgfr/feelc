# Reference example — Benefits / allowances eligibility

Exercises **COLLECT raw (list)**, nested conditions and a **boolean** test. The allowances
are cumulative; the decision returns the list of granted allowances.

## Inputs
- `income` (number, `>= 0`), `children` (number, `[0..15]`), `is_student` (boolean).

## Decision
- **`aids`** (string, `collect`) — list of allowances:
  - `income < 1500` → `"housing"` ; `children >= 1` → `"family"` ;
    `income < 1000` and `is_student = true` → `"student_grant"`.

## Examples
- income 900 / 2 children / student → `["housing", "family", "student_grant"]`.
- income 2000 / 0 children / non-student → `[]`.
- income 1200 / 1 child / non-student → `["housing", "family"]`.

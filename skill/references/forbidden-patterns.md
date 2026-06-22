# Classic pitfalls to avoid

| Pitfall | `verify` symptom | Fix |
|---|---|---|
| **Incomplete single-hit table** (no `default`, domain not covered) | `gap` (error) + counterexample | Add a rule for the missing zone, or an assumed `default` line |
| **Overlap under `unique`** | `conflict` (error) | Tighten the conditions, or switch to `first`/`priority` if ordering/priority is intended |
| **`any` with diverging outputs** | `conflict` (error) | Align the outputs, or change the hit policy |
| **Masked rule** (an earlier rule already covers all of its cases under `first`) | `dead-rule` (warning) | Reorder, or remove the redundant rule |
| **Useless `default`** | `unreachable-default` (info) | OK (safety net) or remove it if the rules are proven complete |
| **FEEL function / `if-then-else` in an expression** | compilation error | Outside the v2 subset — reformulate (table, or intermediate decision) |
| **`sum([...])` to add up cases** | compilation error | Use the `collect sum` hit policy, not a function |
| **Output computed in a cell** | compilation error | Table outputs are literals; compute via a `= <expr>` decision |
| **Comparing a column to another** (`> other_column`) | `not-verifiable` (info) | OK but completeness is not proven on this table (cell Op=Prog) |
| **`not(< 18)`** | compilation error | Replace with the complementary condition (`>= 18`) |

## Determinism
feelc is deterministic by construction: no clock, no randomness, exact decimal. If you want a rule
"based on the date", pass the date/time as a model **input** (the engine never reads the clock
itself).

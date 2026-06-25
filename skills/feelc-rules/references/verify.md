# Reading `feelc verify`

```sh
node scripts/feelc-skill.mjs verify --rules model.rules --json
```

JSON output: `{ "findings": [ { decision, kind, severity, message, witness?, rules? }, … ] }`.
The command **exits 1** if there is at least one finding with `severity: "error"` (blocker), 0 otherwise.

## Severities

- **`error` (blocker)** — must be fixed before considering the model "buildable":
  - `gap`: a case is covered by **no** rule (single-hit table, without `default`).
    `witness` gives a **concrete counter-example** (e.g. `{"n":"45"}`) → add a rule or a `default`.
  - `conflict`: under `unique`, two rules overlap; under `any`, they give different
    outputs. `witness` + `rules` point to the problem.
- **`warning`** (to report, not always to fix):
  - `gap` caught by `default`; `dead-rule` (rule never reachable, or shadowed by an earlier
    rule under `first`). Often a sign of a redundant or badly ordered rule.
- **`info`**:
  - `unreachable-default`: the `default` line is never used (the rules already cover everything).
  - `not-verifiable`: table not geometrically provable (cell `Op=Prog`, i.e. comparison to
    another column, or grid too large). Honest: **not proven**, never silently "compliant".

## Stopping criterion

> **Buildable** = 0 blocker (`error`). **Convergent** = `run` reproduces the reference cases.

Do not remove a rule just to silence a `warning`: first understand *why* it
appears (often an overlap or a row ordering to review). The `error`s must disappear;
the `warning`/`info` are **commented** to the user.

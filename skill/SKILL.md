---
name: feelc-rules
description: >-
  Author, verify and test business rules in the feelc DSL (a DMN/FEEL decision language compiled
  to a deterministic Go engine). Use when the user wants to WRITE or REVIEW business rules /
  decision logic and have them be deterministic, auditable and formally checkable — e.g.
  "encode these eligibility rules", "write a decision table", "rule engine", "DMN", "FEEL",
  "scoring / eligibility / pricing / promotions as rules", "generate business rules",
  "check my decision table", "feelc". The AI writes the .rules source; the feelc
  binary (compile / verify / run) is the deterministic oracle — never decide rule outcomes "in
  your head", always run feelc.
license: Apache-2.0
metadata:
  version: 0.2.0
---

# feelc-rules — writing verifiable business rules

This skill helps you **write, verify and test** business rules in the **feelc** language
(DMN paradigm: a graph of linked decisions, each one a decision table or an expression,
all in FEEL). feelc compiles the source into a **deterministic** engine: at runtime, no LLM,
reproducible and auditable decisions. **Your role = writing the DSL**; the `feelc` binary is
the **deterministic oracle** (compilation + verification + evaluation). NEVER decide a rule
outcome "in your head": always run `feelc`.

## When to use it

- The user wants to encode a business policy (eligibility, scoring, pricing, scale,
  discounts, rights/benefits…) as executable rules.
- They want a **verified** decision table (no gap, no conflict) that is **replayable**.
- They mention DMN, FEEL, "rule engine", or `feelc`.

## Prerequisite: the `feelc` binary

All commands go through the portable wrapper `scripts/feelc-skill.mjs`, which locates
`feelc` (the `FEELC_BIN` variable, PATH, a sibling checkout, or a build via `go build`). Check first:

```sh
node scripts/feelc-skill.mjs version
```

If this fails, the wrapper prints how to provide `feelc` (see `references/install.md`).
You can also call `feelc` directly if it is on the PATH.

## The flow (red → green, driven by the oracle)

Create one todo per step and follow them in order.

1. **Interview** (do not guess). Establish: the **inputs** (Input Data) and their **domains**
   (`in [a..b]`, `>= 0`, `in {…}`), the **decisions** and their dependencies, the **hit policy** of
   each table, the **edge cases**. See `references/authoring.md`.
2. **Write** the `.rules` file. Stay STRICTLY within the supported subset
   (`references/feel.md`) — any out-of-scope construct makes the compilation fail, this is
   intentional. Use the 4 templates in `references/examples.md` as starting points.
3. **Compile + verify** (deterministic gate):
   ```sh
   node scripts/feelc-skill.mjs verify --rules model.rules --json
   ```
   - **Compilation** errors (type, reference, syntax) → with `--json`, they come out as a
     structured object `{file,line,col,code,message,suggestion}` on stdout: use `line`/`col` to
     locate and `suggestion` to fix, then re-run. Stable code catalog:
     `docs/error-schema.md`.
   - Verification **blockers** (`severity: "error"`: completeness gap with counter-example,
     UNIQUE/ANY conflict) → fix. Read `references/verify.md`.
4. **Test** on concrete cases (including the edge cases from the interview):
   ```sh
   node scripts/feelc-skill.mjs run --rules model.rules --decision <name> --input '{…}' --json
   ```
   Compare with the expected result. Iterate until they match.
5. **(Optional) Semantic Layer-2 gate** — verify that the rules really say what the requirement
   wanted. Break down the spec/requirement into atomic **claims** `{decision, input, expect}` (YOUR
   AI work), write them in a `claims.json` (`{"claims":[…]}`), then let the VM decide:
   ```sh
   node scripts/feelc-skill.mjs check --rules model.rules --claims claims.json --json
   ```
   `supported` = the rule confirms the claim; `contradicted`/`error` = blocking (the rule does
   not do what the requirement said, or your claim is false → fix one or the other). "The LLM
   proposes, the VM disposes": never invent a threshold to make a claim pass.
6. **Iterate** until the stopping criterion.

## Stopping criterion: "zero blocker, not zero finding"

- **Buildable** = `verify` returns **no blocker** (`severity: "error"`). The `warning`
  (shadowed rule…) and `info` (useless default…) findings should be **reported**, not necessarily fixed.
- **Convergent** = `run` reproduces the reference/edge cases validated with the user.

Never invent a threshold or a value to "make it pass": if an ambiguity in the requirement
is irreducible, **raise the question with the user**.

## What NOT to do

- ❌ Do not compute a rule result yourself — run `feelc run`.
- ❌ Do not use constructs outside the subset (FEEL functions, `if/then/else` inside an
  expression, regex, dates/timezones, lambdas): it does not compile. See `references/feel.md`.
- ❌ Do not leave a single-hit table (FIRST/UNIQUE/ANY/PRIORITY) **incomplete** without an assumed
  `default`: `verify` will report a gap with a counter-example.
- ❌ Do not dress up a `warning`/`info` as a success, and do not delete a rule just to
  silence a diagnostic without understanding why.

## References (progressive disclosure)

- `references/authoring.md` — the interview and the structure of a model.
- `references/feel.md` — the supported DSL/FEEL subset (and what is not).
- `references/verify.md` — reading `feelc verify`, blockers vs. remarks.
- `references/examples.md` — the 4 reference models as templates.
- `references/forbidden-patterns.md` — classic pitfalls (overlap, missing default…).
- `references/install.md` — providing the `feelc` binary.

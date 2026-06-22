# feelc

> A business-rules pseudo-language (DMN/FEEL) **compiled to Go**, in the spirit of IBM ODM/ILOG —
> with a distinctive angle: **AI helps write and explain the rules, but at execution time everything
> is 100% deterministic, reproducible and auditable** (no LLM in the core).

## Why

Classic rule engines pit *business readability* against *reliable execution*. `feelc` reconciles
the two:

- **AI writes, the engine executes.** Rules are written in a readable `.rules` DSL (DMN paradigm:
  a graph of decisions, each a decision table, expressions in FEEL). An LLM can generate it
  natively. The Go compiler transforms it into typed, checked IR, executed by a small deterministic VM.
- **Formal verification.** `feelc verify` proves **completeness** (no uncovered case),
  the **absence of conflicts**, and detects **dead rules / redundancies** — with concrete counterexamples.
- **Hot-reload.** Rules are *data*: you update them on the fly, without recompiling the binary.
- **Auditable.** Each decision is replayable (model hash + explanation trace citing the source).

## Commands

```sh
feelc run     --rules m.rules --decision <name> --input '{…}' [--json]  # evaluate a decision
feelc compile --rules m.rules [-o m.ir.bin]                             # compile to canonical IR
feelc verify  --rules m.rules [--json]                                  # formal check (gaps/conflicts)
feelc explain --rules m.rules --decision <name> --input '{…}' [--json]  # justification trace
feelc check   --rules m.rules --claims claims.json [--json]             # NL↔rule semantic gate
feelc fmt     --rules m.rules [-w] [--check]                            # canonical pretty-printer
feelc import  --in model.dmn  [-o m.rules]                              # import DMN XML
feelc export  --rules m.rules [-o model.dmn]                            # export to DMN XML
feelc tck     --suite <dir>   [--json] [--min <pct>]                    # DMN TCK conformance
feelc serve   --rules m.rules [--addr :8080] [--watch] [--strict]       # HTTP service + hot-reload
```

## Status

Core **operational**: language → compiler → IR → deterministic VM (exact decimal), 7 hit policies,
**formal verification** (completeness/conflicts/dead rules with counterexamples), **HTTP service +
hot-reload**, **semantic gate** (`check`), **DMN XML import**. 4 verified reference examples.
Deferred (ADR 0004): parameterized BKM, SMT/Z3 extension.

## Authoring skill (AI writes the rules)

A **portable skill** (Claude Code, Codex, Cursor…) is bundled in [`skill/`](skill/): it
guides an agent in writing/verifying rules through the *interview → DSL → `verify` → `run` →
iterate* flow, using `feelc` as a deterministic oracle. See [`skill/SKILL.md`](skill/SKILL.md).

```sh
node skill/scripts/feelc-skill.mjs verify --rules examples/credit/credit.rules --json
```

## Example

```
model "credit" {
  rounding: half_even
}

input credit_score  : number in [300..850]
input annual_income : number >= 0
input monthly_debt  : number >= 0
input age           : number in [0..120]

decision dti : number = monthly_debt / (annual_income / 12)

decision eligibility : Eligibility {
  needs: credit_score, dti, age
  hit: first
  #  credit_score | dti     | age   => eligible | reason
     < 580        | -       | -     => false    | "insufficient score"
     -            | > 0.43  | -     => false    | "debt too high"
     -            | -       | < 18  => false    | "minor"
     [580..680)   | <= 0.43 | >= 18 => true     | "approved with conditions"
     >= 680       | <= 0.43 | >= 18 => true     | "approved"
     default      |         |       => false    | "not covered"
}
```

## License

Apache-2.0. See [LICENSE](./LICENSE).

# feelc

> An **AI-driven rule engine**: a business-rules language (DMN/FEEL) **compiled to Go**, in the
> spirit of IBM ODM/ILOG — with a distinctive angle: **AI writes and explains the rules, but at
> execution time everything is 100% deterministic, reproducible and auditable** (no LLM in the core).
> Author rules by chatting with your own LLM, visualize the decision graph, and let the engine prove
> completeness and consistency.

**▶ [Try the playground](https://maxgfr.github.io/feelc/playground/)** — the real engine, compiled to
WebAssembly, runs entirely in your browser (no backend). Or [read the docs](https://maxgfr.github.io/feelc/).

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
feelc graph   --rules m.rules [--format mermaid|dot|json]               # decision graph (DRG) + findings
feelc inputs  --rules m.rules --decision <name>                         # inputs a decision needs (question-flow)
feelc docs    --rules m.rules [-o DOC.md]                               # Markdown reference + Mermaid graph
feelc serve   --rules m.rules [--addr :8080] [--watch] [--strict] [--ui] # HTTP service + hot-reload (+ AI UI)
feelc serve   --project <dir>  [--addr :8080] [--watch] [--strict] [--ui] [--allow-edit] # multi-module PROJECT
```

## Project mode

For more than a single file, a **project** is a directory of `.rules` modules plus an optional
`feelc.project.json` manifest. The modules are namespaced (`module__decision`) and **linked into one
deterministic model** the engine runs unchanged — a single hash, one verification pass, one decision
graph. Modules reference each other through a manifest `uses` binding (a local input wired to
`other.decision`); cross-module cycles and dangling refs are rejected at load. The web UI (`--ui`) gains
a module navigator, a project health dashboard, and a cross-module graph; adding **`--allow-edit`**
(off by default — the editing surface is unauthenticated, so keep it on a trusted/loopback host) enables a
per-module editor with server-side **Save** + create/delete, persisted to the mounted directory under a
golden rule: an invalid edit is rejected and the live project is kept. See the
**[project-mode guide](docs/project-mode.md)** (also on
the [docs site](https://maxgfr.github.io/feelc/docs/project-mode.html)), [`sample-project/`](sample-project/),
and ADR 0015. Ships as a Docker service:

```sh
docker build -t feelc .
docker run --rm -p 8080:8080 -v "$PWD/sample-project:/work" feelc   # open http://localhost:8080/
```

## Status

Core **operational**: language → compiler → IR → deterministic VM (exact decimal), 7 hit policies,
**formal verification** (completeness/conflicts/dead rules with counterexamples), **HTTP service +
hot-reload**, **semantic gate** (`check`), **DMN XML import**. 4 verified reference examples.
Optional **SMT (Z3) backend** (`-tags smt`, ADR 0007) discharges non-geometric `Op=Prog`
residuals — completeness *and* conflict proofs over `if/then/else`, `floor/ceiling/round`,
cross-column cells (honest `not-verifiable` when `z3` is absent).

Modelling reach (inspired by Publicodes & Catala): **decision-graph visualization** (`feelc graph`
+ the UI, ADR 0009), **rule metadata & law/source traceability** (`@title/@doc/@question/@source`,
ADR 0010), an **interactive question-flow / simulator** (ADR-backed `feelc inputs` + the UI form),
**progressive brackets** (`bracket:`, ADR 0011), **physical units & money** with compile-time
dimensional analysis (ADR 0012), **applicability** (non-applicable values, ADR 0013), and **date /
duration** types with sound whole-day arithmetic (ADR 0014). Deferred: multi-arg built-ins
(ADR 0004 §3) and out-of-subset temporal forms (times of day, date-times, year-month durations).
Generate docs with `feelc docs` (Markdown + Mermaid graph), or scaffold a cited repo reference with
the external [ultradoc](https://github.com/maxgfr/ultradoc) skill.

## AI writes the rules (two paths)

Per the thesis — **AI authors, the engine executes** — there are two interchangeable authoring paths.
The LLM only drafts `.rules`; every result you see comes from the deterministic engine
(see [ADR 0008](docs/adr/0008-ai-authoring-layer.md)).

**1. In-browser chat UI (bring-your-own LLM).** `feelc serve --ui` serves a zero-dependency embedded
UI: chat to describe your rules, the assistant drafts the model, and one click runs `verify` / `run`
on the deterministic engine. It also renders the **decision graph**, builds a **simulator form** that
asks only the inputs a decision needs, narrates a result in **plain English** ("Explain"), and
**generates test cases** that are then checked deterministically. Configure your own
provider/model/API key in ⚙ settings (Anthropic or any OpenAI-compatible endpoint). The key stays in
your browser and transits only your local server; it is never stored or logged. With no key (request
or `ANTHROPIC_API_KEY` env), AI endpoints return `501` and the engine still works.

```sh
feelc serve --ui            # then open http://localhost:8080/ (no --rules needed)
```

**2. Claude Code + the bundled skill.** A **portable skill** (Claude Code, Codex, Cursor…) is bundled
in [`skill/`](skill/): it guides an agent through the *interview → DSL → `verify` → `run` → iterate*
flow, using `feelc` as a deterministic oracle. See [`skill/SKILL.md`](skill/SKILL.md).

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

# Comparison & gap analysis

How feelc compares to the major business-rule engines, what it deliberately does **not** do, and the
**philosophy-compatible** language features worth adding. Based on a source-level analysis of
[json-rules-engine](https://github.com/CacheControl/json-rules-engine),
[json-logic-js](https://github.com/jwadhams/json-logic-js),
[GoRules ZEN](https://github.com/gorules/zen),
[node-rules](https://github.com/mithunsatheesh/node-rules), and the DMN 1.3 / FEEL standard (Drools).

## Where feelc leads

| Capability | feelc | json-rules-engine | json-logic | GoRules ZEN | node-rules | DMN/Drools |
|---|---|---|---|---|---|---|
| **Verification in the engine** (completeness, conflict, subsumption, with counterexamples) | ✅ proven, in-engine | ❌ | ❌ | ❌ | ❌ | ⚠️ modeler-only¹ |
| **Exact decimals** (no float drift) | ✅ | ❌ float | ❌ float | ✅ | ❌ float | ✅ |
| **Determinism / replayability** (pure, no I/O, hashed model) | ✅ | ⚠️ async facts | ✅ | ⚠️ JS nodes | ⚠️ restart loop | ✅ |
| **DMN hit policies** (U/A/P/F/R/C + collect agg; DMN also defines OUTPUT ORDER) | ✅ 6/7 (no OUTPUT ORDER) | ❌ | ❌ | ⚠️ first+collect | ❌ | ✅ all 7 |
| **Progressive brackets** (marginal-rate schedules) | ✅ primitive | ❌ | ❌ | ⚠️ hand-rolled | ❌ | ⚠️ hand-rolled |
| **Units / dimensional analysis** (EUR + EUR/month = compile error) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Applicability / non-applicable sentinel** (distinct from null/0) | ✅ | ❌ | ❌ | ❌ | ❌ | ⚠️ |
| **Static decision graph** (DRG, cycles rejected at compile time) | ✅ | ⚠️ dynamic | ❌ | ✅ | ⚠️ dynamic | ✅ |
| **Portability** (browser, Node, edge, Docker, WASM) | ✅ | ✅ JS | ✅ JS | ✅ | ✅ JS | ❌ JVM |
| **DMN XML interop** | ✅ import/export | ❌ | ❌ | ❌ | ❌ | ✅ |

feelc's north star is **verifiability**. Among lightweight, **code-first** engines (json-rules-engine,
json-logic, ZEN, node-rules) it is the only one that *proves* a decision table complete and conflict-free
at compile time — the rest resolve gaps/overlaps at runtime.

¹ **In fairness:** DMN *platforms* — Camunda's dmn-js "verify table", Trisotech's *Method & Style*
decision-table analysis, and Drools/DMN — **do** offer gap/overlap analysis (the academic foundation is
[Calvanese et al., *Semantics and Analysis of DMN Decision Tables*](https://arxiv.org/pdf/1603.07466)). So
feelc is **not** the only system on earth that checks tables. Its distinction is *where* and *how*: the
verification (completeness + conflict + **subsumption**, with counterexamples, SMT-extensible) is built
into the compiler of a **single portable Go binary / WASM module** with **exact decimals** — combining
DMN-grade analysis with code-first ergonomics and edge portability, rather than a heavyweight JVM/SaaS
modeling tool.

## What feelc deliberately rejects (out of scope, not defects)

These are the load-bearing features of the interpreted competitors. Each breaks determinism,
replayability, or static verification — the reason feelc exists — so the architecture is: **the host
does I/O, normalization, regex and list pre-aggregation; feelc receives flat typed inputs and returns
verified decisions.**

- **Async / dynamic facts** (fetch data mid-evaluation) — json-rules-engine. Resolve to typed inputs first.
- **Custom JS operators / Function nodes / `add_operation`** — ZEN, node-rules, json-logic. Defeats verification.
- **Runtime rule/operator mutation** — feelc hot-reloads & re-verifies whole modules instead.
- **Unbounded iteration / higher-order list ops** (`map`/`reduce`/`filter`/`some`/`every` over runtime-sized lists) — undermines the geometric completeness proof.
- **Regex & string-manipulation libraries** (`substr`, `cat`, `replace`, `split`) — strings are opaque tokens.
- **Side effects / event-action emission** — feelc is a pure decision function; the host switches on its output.
- **Sub-day / time-of-day / year-month durations** — reintroduce timezone/calendar ambiguity; whole-day only (ADR 0014).

## Shipped since this analysis ([ADR 0020](adr/0020-deterministic-extra-builtins.md))

The quick wins are now implemented — deterministic, exact, and verification-safe:
**`round(x, n)`**, **`abs(x)`**, **`trunc(x)`**, and **`modulo(x, y)`** (FEEL-standard function, DMN
floored semantics). Their tripwire tests flipped from *rejected* → *supported*.

## Gaps still open (philosophy-compatible)

Ranked. Each is deterministic and verifiable — these would widen coverage **without** weakening any
guarantee. Each has a live tripwire test (`packages/engine/test/conformance.test.ts`).

| # | Gap | Impact / effort | Recommendation |
|---|-----|-----------------|----------------|
| 1 | **Bounded** quantifiers over a fixed-arity input tuple (`every of {a,b,c} satisfies ? < 26`) | high / medium | Keeps the input space hyper-rectangular → verification stays sound. Recovers most "every dependant"/"sum line items" cases **without** a list type. |
| 2 | `starts_with` / `ends_with` / `contains` as **cell tests** | medium / medium | Pure, total, decidable. Covers code/policy-number routing. Not a string-building library. |
| 3 | Expression-level Kleene (3-valued) null logic | medium / medium | Closer DMN alignment; weigh against feelc's "fail loudly" stance (consider opt-in). |
| 4 | Downstream **read** of an upstream context field (`result.rate`) | medium / high | Removes DRG-composition friction while keeping inputs flat/typed. |
| 5 | Integer-exponent `**` only | low / low | Optional; general real exponents risk precision blow-up, so integer-only stays exact. |

Each remaining addition must ship with an append-only ADR and flip its tripwire test from "rejected"
to "supported".

## Why feelc is unusually AI-friendly

LLMs are great at *writing* rules and unreliable at *deciding outcomes*. feelc is built for exactly that
split — the AI writes the `.rules`; the deterministic engine is the oracle:

- **Structured, positioned diagnostics.** Every compile error is `{file, line, col, code, message,
  suggestion}` (JSON) — an LLM reads it and self-corrects instead of guessing. Stable code catalog
  ([error-schema.md](error-schema.html)).
- **A verification oracle, not vibes.** `verify` *proves* completeness/conflict/subsumption with concrete
  counterexamples, and `check` runs the spec's claims `{decision, input, expect}` against the model. "The
  LLM proposes, the VM disposes" — the model can't hallucinate a rule outcome.
- **Deterministic & exact at runtime.** No LLM in the decision path: same inputs → same auditable output,
  bit-for-bit, replayable. Generated rules are trustworthy because the *engine*, not the model, executes.
- **A ready agent skill.** `skill/` is a portable Claude/Codex/Cursor skill with the red→green authoring
  loop and references; the `feelc` binary is the deterministic tool.
- **Runs anywhere the agent's app runs.** `@feelc/engine` (WASM) executes the generated rules in the
  browser/Node/edge with no server — byte-identical to the CLI ([embedding.md](embedding.html)).
- **Reviewable artifacts.** `.rules` is plain text, git-diffable, and compiles to a content-hashed
  `.ir.bin` (the hash *is* the model identity) — every AI edit is auditable and re-verified.

## How the comparison is kept honest

The claims above are backed by executable tests: `examples/node-smoke` proves the WASM engine matches
the native CLI across every example + corpus decision, and `packages/engine/test/conformance.test.ts`
asserts both the supported semantics and the current rejection of every out-of-scope construct.

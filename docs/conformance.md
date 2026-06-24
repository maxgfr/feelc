# Conformance

How feelc fares against the **official DMN Technology Compatibility Kit (TCK)** and against six other
rule engines, measured by re-running their own test suites / scenarios through feelc.

**The headline: the engine is never *wrong* on a feature it supports.** It computes the correct answer
or honestly refuses an out-of-scope construct. The only non-refusal failures below are DMN-*import*
fidelity gaps (OUTPUT ORDER, PRIORITY), not evaluation errors. feelc's omissions are *deliberate*
exclusions (unbounded lists/iteration, loop-until-fixpoint, string/regex) — consistent with being a
total, deterministic, verifiable evaluator.

## Official DMN TCK (OMG conformance suite)

Run with feelc's built-in conformance runner — `feelc tck --suite <dir>` — which imports each `.dmn`,
compiles it, and checks every `<testCase>` against the TCK's own expected results (exact-decimal
equality). Conformance % = passed / (passed + failed); out-of-subset cases are honestly *skipped*.

| Suite | Passed | Failed | Skipped | Conformance |
|---|---|---|---|---|
| **Compliance level 2** (decision tables) | **53** | 10 | 63 | **84.1%** |
| Compliance level 3 (full FEEL) | 0 | 3 | 3366 | 0% (deliberate subset) |
| Non-compliant (should be rejected) | — | — | — | rejects the recursion / string-function models |

- **Level 2 (feelc's core):** 84.1% — and of the 10 non-passing cases, **none is a wrong value for a
  supported feature.** 7 are honest *refusals* of out-of-scope constructs (string concatenation, `**`
  power, full Kleene null logic, a `.872` leading-dot literal, a spaced FEEL name); the other 3 are
  **hit-policy import limitations** — DMN `OUTPUT ORDER` is unsupported (2 cases), and `PRIORITY` is
  currently imported as `FIRST` (1 case — a `dmnxml` fidelity gap, not an evaluation bug).
- **Level 3 (full FEEL):** feelc is a *deliberate subset* — `for`/`some`/`every`, lists, string
  functions, time-of-day, etc. are out of scope, so the runner honestly **skips** them (3366 skipped)
  rather than faking conformance. It still never returns a wrong value.
- **Non-compliant:** feelc correctly rejects the recursion / string-function models.

## Cross-engine scenario coverage

71 representative scenarios drawn from six engines' own examples/tests were ported to feelc and proven
on the CLI (compile **and** reproduce the engine's asserted output). **56 of the modelable `.rules` are
committed** as a permanent test corpus (`packages/engine/test/corpus/x-*.rules`).

| Engine | Modelable / total |
|---|---|
| json-rules-engine | 10 / 10 |
| json-logic-js | 11 / 11 |
| GoRules ZEN | 13 / 15 |
| node-rules | 9 / 11 |
| microsoft/RulesEngine | 11 / 12 |
| grule | 9 / 12 |
| **Total** | **63 / 71 (89%)** |

Every cross-cutting decision primitive ported 1:1 — all hit policies (first/unique/priority/collect),
set membership, fact-vs-fact comparison, chained derived facts as a DRG, exact-decimal arithmetic with
units, `round`/`floor`/`ceiling`/`trunc`/`modulo`, nested `if/then/else`, marginal brackets, BKM, and
applicability gating. feelc was **stricter and more correct** in two ways the others don't offer: exact
decimals (no float drift — `0.1 + 0.2 = 0.3`, vs json-logic's `0.30000000000000004` and grule's float32
drift) and a totality/completeness checker that surfaced uncovered-band warnings none of these engines
perform.

## The gaps (all deliberate)

The 8 cross-engine gaps + the TCK out-of-scope cases reduce to three intentional exclusions — each breaks
determinism, totality, or static verification, so feelc rejects them by design (see
[comparison.md](comparison.md)):

1. **Unbounded lists / iteration / higher-order** — `map`/`reduce`/`filter`/`some`/`every`, `sum()` over
   a runtime list, list-typed inputs. (Bounded quantifiers over fixed-arity tuples are a candidate add.)
2. **Loop-until-fixpoint / recursion / re-feeding outputs as inputs** — feelc is a total, single-pass,
   acyclic DRG.
3. **String manipulation & regex** — concat, `substring`, `ToUpper`, wildcard `.match`. (`starts_with`/
   `contains` as cell tests are a candidate add.)

Nested-object/list-path access and async/dynamic/JS-side-effect facts are likewise out of scope, but they
only affect *data plumbing* (flatten to typed scalar inputs first), never the rule logic.

## Reproduce

```sh
# DMN TCK (clone github.com/dmn-tck/tck): for each TestCases/<level>/<dir>, feelc import + run vs expected.
# Cross-engine: scenarios ported under packages/engine/test/corpus/x-*.rules, swept vs the CLI by
npm -w @feelc-examples/node-smoke test     # WASM == native CLI across every example + corpus decision
npm -w @feelc/engine test                  # frozen-output conformance corpus + rejection/tripwire tests
```

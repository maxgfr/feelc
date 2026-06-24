# Competitive benchmark report

*Generated 2026-06-24. A reproducible head-to-head of feelc against six other rule engines on one
identical decision, run on a single Apple Silicon (darwin/arm64) host. This complements the feature
matrix in [comparison.md](comparison.md) — that document compares *capabilities*; this one compares
*measured throughput* on the same workload.*

Every engine — Node, Go, Rust (via binding), and two JVM DMN engines — was cloned/installed at a
released version, given the **same** 6-row priority-cascade decision and the **same** 200,000
deterministic input rows, warmed up identically, and verified against a shared correctness checksum
(`2,663,397`). They all compute the *same answers*; they differ only in paradigm and binding cost.

The honest one-line takeaway: a bare tree-walking JSON interpreter (json-logic-js) wins raw µs on this
*trivial* workload, but **feelc's native compiled-table path is the second-fastest measured and the
fastest engine that also provides in-engine static verification, exact decimals, deterministic
evaluation, and a cgo-free single binary + portable WASM** — a small constant factor traded for
correctness guarantees the faster interpreters do not offer.

---

## Discount-Decision Benchmark: feelc vs. Other Rule Engines

A single, identical decision was implemented in every engine: a 6-row priority cascade (`amount>=500 & member → 20`; `amount>=500 → 15`; `amount>=100 & member → 12`; `amount>=100 → 10`; `amount>=50 → 5`; else `0`), evaluated FIRST-match. The same 200,000 deterministically generated rows (`amount=(i*7)%1001`, `member=(i%2==0)`) were run through each engine after a 10,000-eval untimed warmup, single-threaded, on the same Apple Silicon (darwin/arm64) host. Every engine produced the identical correctness checksum (sum of discounts = 2,663,397), so all rows below are computing the *same answers* — they differ only in paradigm and binding cost.

### Comparison table

| Engine | Language / runtime | eval/sec | µs/op | Startup | Paradigm | Notes |
|---|---|---:|---:|---:|---|---|
| **feelc (native)** | Go (native, `EvalTable`) | ~1,550,000 | ~0.65 | ~0 | Compiled decision table | Reference; no binding boundary. |
| **feelc (WASM batch)** | Go→WASM (Node 24, `evaluateBatch`) | ~144,000 | ~6.95 | n/a | Compiled table, batched JSON boundary | 2.1× the per-call WASM path; amortizes the JS↔WASM crossing. |
| **feelc (WASM per-call)** | Go→WASM (Node 24, `evaluate`) | ~68,000 | ~14.6 | n/a | Compiled table, per-call JSON boundary | Per-op cost dominated by the JSON boundary, not the rule. |
| json-logic-js 2.0.5 | JavaScript (Node 24.10.0) | 2,721,522 | 0.367 | ~0.017* | Tree-walking JSON interpreter | Fastest measured, but apples-to-oranges: re-interprets the rule tree every call; "startup" is a meaningless JSON round-trip (no compile step). Zero deps, 92K on disk. |
| GoRules ZEN 0.54.0 | Rust core via Node napi (prebuilt) | ~25,000 (seq) / ~170,000 (batched) | ~40 (seq) / ~5.8 (batched) | ~3 | Compiled JDM graph (decision table + expr VM) | Async napi binding only; **no sync evaluate**. Sequential await-per-row is round-trip-bound (high variance, 20.8k–29.6k eps); concurrent `Promise.all` batches reveal the Rust core is materially faster. Native `first` hit policy. |
| json-rules-engine 7.3.1 | JavaScript (Node 24.10.0) | 71,483 | 13.99 | 0.26 | Async forward condition-eval (almanac) | `await engine.run()` per op — microtask round-trip dominates. Evaluates ALL rules every run (no short-circuit); FIRST emulated via priority + `events[0]`. 3-run range 68.7k–74.0k eps. |
| Drools DMN 9.44.0 | Java 21 (HotSpot, Docker) | 328,022 | 3.05 | 212 | DMN 1.3 decision table (FEEL, decimal) | Warm-JIT steady state; cold one-shot far worse. Decimal `BigDecimal` like feelc; fresh `DMNContext`+`DMNResult` alloc per row. 3-run range 306.9k–330.3k eps. |
| grule 1.20.4 | Go (native, go1.26) | 525,219 | 1.904 | 1.254 | Forward-chaining production engine (RETE-style) | Interprets an ANTLR AST per eval; FIRST emulated via salience + `Complete()`. Per-op cost is largely reflection + working-memory reset, not rule logic. 2-run spread ~0.05%. |
| Camunda DMN 7.21.0 | Java 21 (HotSpot, Docker) | 4,124 | 242.5 | 173 | DMN 1.3 table, Scala FEEL interpreter (decimal) | Re-parses FEEL unary-test strings (`">= 500"`) per cell per row; warm-JIT. Harmless `DMN-01006` (untyped `number`) disables the typed fast-path. 2-run range 4,116–4,124 eps. |

\* json-logic-js has no compile step; its "startup" figure is a JSON round-trip added only so there was something to time, and is not comparable to engines that build a model.

### Reading the numbers

These results compare **different paradigms doing the same job**, and the per-op figure is frequently dominated by *binding and scheduling overhead rather than rule evaluation* — so the ranking is not a clean "which engine is fastest" verdict. Concretely:

- **Micro-benchmark bias.** This is a tiny 6-rule, single-decision, scalar workload. It flatters lightweight interpreters (json-logic-js wins outright at 0.367 µs/op precisely because the rule tree is trivially small) and penalizes engines built for a different problem. grule and json-rules-engine are forward-chaining/production engines designed for *many rules over many facts with incremental matching* — on a single tiny decision their machinery (RETE-style conflict resolution, almanac, per-call working-memory reset) is pure overhead, so these numbers say nothing about their many-rule scaling.
- **JVM JIT warmup.** Drools (3.05 µs) and Camunda (242.5 µs) report *warm steady-state* after 10,000 untimed evals; a cold one-shot CLI invocation would be far slower (and excludes ~170–210 ms of engine build plus full JVM boot/classload, reported separately as startup). Treat these as order-of-magnitude signals, not tuned JMH figures (no fork isolation, no blackhole; run under Docker on macOS).
- **Binding overhead, not core speed.** Several rows measure a *boundary*, not an engine. ZEN's sequential ~25k eps jumps to ~170k eps under concurrent batching with an identical checksum — proving the napi/Promise round-trip, not the Rust core, bounds the sequential number; the true Rust core is faster still. The same caveat applies to **feelc's own WASM rows**: per-call WASM (14.6 µs) is JSON-boundary-dominated, and batching recovers 2.1× (6.95 µs) — feelc's native path (0.65 µs) is the honest measure of the compiled engine, the WASM numbers measure the JS↔WASM crossing. json-rules-engine's 13.99 µs is similarly an `async`/microtask cost, not condition-walking cost.
- **Decimal vs. double.** Drools, Camunda, and feelc compute in **exact decimals** (BigDecimal / FEEL number semantics); json-logic-js and grule use native floating-point/typed numbers. Decimal arithmetic is correctness-relevant (money) and inherently costs more per op, so the FEEL/DMN engines are not strictly comparable on raw speed to the float engines.
- **Don't over-read the ranking.** json-logic-js is fastest here but is a pure tree-walking interpreter with no verification, no compile step, and no decimal semantics. feelc's native compiled-table path (~0.65 µs/op, ~1.55M eval/sec) is the second-fastest measured and the fastest engine that also offers **in-engine static (SMT) verification, exact decimals, deterministic evaluation, and a cgo-free single static binary plus a portable WASM build** — i.e. it trades a small constant factor vs. a bare interpreter for correctness guarantees the interpreters do not provide.

**Engines not measured:** none in this round failed to build — all six competitors plus feelc's three paths produced the correct checksum and are reported above. (Variance and methodology caveats are stated per row rather than hidden; where an engine had run-to-run spread, the median run is reported and the full range is shown.) Commercial/closed engines in the feature matrix (Microsoft RulesEngine [.NET], OpenRules, DecisionRules) were not throughput-benchmarked here.

### Methodology & reproducibility

Identical decision logic, identical 200,000-row deterministic input (`amount=(i*7)%1001`, `member=(i%2==0)`), 10,000-eval untimed warmup, single-threaded timed loop, same Apple Silicon host; each engine pinned to a released version, verified against the shared checksum 2,663,397, and run 2–3× with median reported and ranges disclosed — numbers are machine-dependent and indicative of order-of-magnitude, not a tuned JMH/cross-host benchmark. feelc's own rows come from `go test -bench` (native) and `packages/engine/scripts/bench-batch.mjs` (WASM per-call + batch).

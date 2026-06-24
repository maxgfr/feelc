# Benchmarks

Honest performance numbers. feelc is a compiled bytecode VM in Go, so it is **very fast natively**; in
JavaScript it runs as WebAssembly and pays a JSON-marshalling cost per call across the JS↔WASM boundary.
The figures below are illustrative (Apple Silicon, single core) — reproduce with the commands shown.

## Native Go (the real engine speed)

`go test -bench=. -benchmem ./internal/engine/` — compile once, evaluate many (the served-model / hot path):

| Benchmark | ns/op | ≈ evals/sec | allocs/op |
|---|---|---|---|
| `EvalTable` (collect-max decision table) | ~645 ns | **~1.55 M** | 7 |
| `EvalExpr` (arithmetic + BKM + `round`/`abs`) | ~1.3 µs | ~750 K | 36 |
| `RunTable` (cold: parse + compile + eval each time) | ~9 µs | ~110 K | 125 |

Sub-microsecond per decision once compiled. This is the number that matters for a Go service, a Docker
sidecar, or a server embedding the engine. Exact-decimal arithmetic is included (no float shortcuts).

**Profiling note.** A CPU profile of the hot path shows it is already tight: `EvalTable` is 7 allocs /
880 B per op, and the engine functions (`vm.resolve`, `vm.matches`, `ir.MatchCell`) are a small slice of
runtime — the rest is GC/scheduler overhead. There is no high-ROI, determinism-safe micro-optimization
to chase here; the real per-op cost (the WASM JSON boundary) was addressed by the batch API above.
Deeper native micro-opts are deliberately deferred — they would trade the exact-decimal / bit-for-bit
determinism contract for marginal gains.

## JavaScript head-to-head (same "best discount" rule)

`/tmp` micro-benchmark: the identical rule evaluated in feelc (WASM), json-logic-js, and
json-rules-engine. **All three produce identical outputs.**

| Engine | eval/sec | µs/op | notes |
|---|---|---|---|
| json-logic-js | ~1.9 M | 0.52 | pure JS, operates on JS objects, **no marshalling**; trivial expression |
| json-rules-engine | ~103 K | 9.7 | async pipeline overhead |
| **feelc (WASM, compiled)** | ~68 K | 14.7 | engine is sub-µs; the cost is **JSON marshalling across the WASM boundary** (stringify input + parse output per call) |

**Reading this honestly:** for a *trivial* rule, a pure-JS interpreter (json-logic) wins on raw
throughput because it has no boundary to cross — feelc's ~14 µs is almost entirely the per-call JSON
round-trip, not evaluation (the Go bench shows the engine itself at ~0.6 µs). feelc is in the same order
of magnitude as json-rules-engine. The boundary cost is **fixed per call**, so it amortizes as rules get
larger (more decisions per call).

### Batch evaluation removes most of the boundary cost (ADR 0024)

`CompiledModel.evaluateBatch(decision, rows[])` evaluates N input rows in **one** JS↔WASM crossing
(one JSON parse, one handle lookup). Since the boundary — not the evaluation — dominates the per-call
cost, batching amortizes it. Measured on the `promo` model, 20 000 rows (Apple Silicon):

| Path | Node 24 | Bun 1.3 |
|---|---|---|
| `evaluate()` per row | 14.4 µs/row | 13.1 µs/row |
| `evaluateBatch()` | **6.95 µs/row** | **5.59 µs/row** |
| speed-up | **2.1×** | **2.3×** |

The win grows with batch size and shrinks with per-row work; for reactive UIs and bulk scoring it is the
recommended path. Reproduce: `node packages/engine/scripts/bench-batch.mjs` (or `bun`), or see
`packages/engine/test/engine.test.ts`.

### Head-to-head vs other engines

For one identical decision benchmarked across feelc + **6 other engines** (json-logic-js,
json-rules-engine, grule, GoRules ZEN/Rust, Drools & Camunda on the JVM) on the same host — with full
per-engine caveats — see the [competitive benchmark report](competitive-report.md). Short version:
feelc's native compiled-table path (~0.65 µs/op) is the **second-fastest measured and the fastest engine
that also offers in-engine verification, exact decimals, and cgo-free portability**; a bare JSON
interpreter is faster only because it skips all of that.

## What you actually trade for

feelc does not win the trivial-rule JS micro-benchmark, and that's the honest picture. What it buys
instead — none of which the faster interpreters provide — is: **exact decimals** (json-logic and
json-rules-engine use IEEE-754 floats), **determinism/replayability**, **static verification**
(completeness/conflict/subsumption proofs), and **byte-identical results across Go, the CLI, and every
JS/edge host**. For throughput-critical paths, run it natively in Go (sub-µs); for portability, run the
same engine as WASM.

## Reproduce

```sh
go test -bench=. -benchmem -run='^$' ./internal/engine/   # native Go
# JS head-to-head: npm i feelc json-logic-js json-rules-engine && node bench.mjs
```

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
larger (more decisions per call) and is removable with a batch-evaluate API (a documented optimization).

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
# JS head-to-head: npm i @feelc/engine json-logic-js json-rules-engine && node bench.mjs
```

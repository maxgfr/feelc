# ADR 0024 — Batch-evaluate API (`evaluateBatch`)

- **Status**: Accepted (2026-06-24)
- **Deciders**: maxgfr

## Context

The Go engine evaluates a compiled decision table in ~0.6 µs. In JavaScript (via WASM) the *measured*
cost is ~14.7 µs/op — and the gap is almost entirely the **JSON marshalling across the JS↔WASM
boundary** (stringify the input, parse the output) on **every call**, not evaluation
([benchmarks.md](../benchmarks.md)). For reactive UIs and bulk scoring (re-evaluating one model over
many rows), that fixed per-call overhead is paid N times even though the model and handle never change.

## Decision

Add a **batch evaluation** entry point that crosses the boundary **once** for N rows.

- WASM: `evalCompiledBatch({handle, decision, inputs:[...], explain?, full?})` →
  `{decision, results:[...]}` (`cmd/feelc-wasm/main_wasm.go`). One JSON decode (with `UseNumber()` for
  decimal exactness), one handle lookup, then the **same per-row `evalResult`** the single-call path
  uses — so a batch row is byte-identical to `evalCompiled`. A fresh evaluator per row (no shared
  memo/state) preserves determinism. A row that errors becomes a `{error}` entry instead of failing the
  whole batch.
- TS: `CompiledModel.evaluateBatch(decision, inputs[], opts?)` → `BatchResult`
  (`packages/engine/src/engine.ts`, types in `types.ts`).

## Consequences

- **Measured 2.1× (Node 24) / 2.3× (Bun 1.3)** on 20 000 rows of the `promo` model: 14.4 → 6.95 µs/row,
  13.1 → 5.6 µs/row. The win grows with batch size and shrinks with per-row work. It is purely an
  amortization of the boundary; the engine path is unchanged.
- Additive and backward-compatible — `evaluate()` is untouched; the precompiled `.ir.bin` handle model
  is unchanged.
- No determinism or exactness change: each row goes through the identical `engine.Eval` → `evalResult`
  path, `UseNumber()` keeps input decimals exact.
- Tests: `packages/engine/test/engine.test.ts` (`evaluateBatch` parity + bad-row isolation); benchmark
  harness `packages/engine/scripts/bench-batch.mjs` (Node + Bun).

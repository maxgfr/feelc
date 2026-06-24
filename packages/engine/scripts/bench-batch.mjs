// Micro-benchmark: evaluate() per row vs evaluateBatch() (ADR 0024). Run with `node` or `bun`:
//   node packages/engine/scripts/bench-batch.mjs
//   bun  packages/engine/scripts/bench-batch.mjs
// Requires a built package (`npm run build -w feelc`).
import { createEngine } from "../dist/index.js";

const MODEL = `model "promo" {}
input cart_total : number >= 0
input is_member  : boolean
decision discount_pct : number {
  needs: cart_total, is_member
  hit: collect max
  >= 50  | -    => 5
  >= 100 | -    => 10
  -      | true => 8
}`;

const engine = await createEngine();
const model = engine.compile(MODEL);
const N = Number(process.env.N ?? 20000);
const rows = Array.from({ length: N }, (_, i) => ({ cart_total: i % 200, is_member: i % 2 === 0 }));

for (let i = 0; i < 200; i++) model.evaluate("discount_pct", rows[i]); // warmup
model.evaluateBatch("discount_pct", rows.slice(0, 200));

let t = performance.now();
for (const r of rows) model.evaluate("discount_pct", r);
const single = performance.now() - t;

t = performance.now();
const batch = model.evaluateBatch("discount_pct", rows);
const batchMs = performance.now() - t;
if (batch.results.length !== N) throw new Error("batch length mismatch");

const rt = process.versions.bun ? `bun ${process.versions.bun}` : `node ${process.versions.node}`;
console.log(`runtime: ${rt}, rows: ${N}`);
console.log(`evaluate()      : ${((single / N) * 1000).toFixed(2)} µs/row  (${single.toFixed(0)} ms)`);
console.log(`evaluateBatch() : ${((batchMs / N) * 1000).toFixed(2)} µs/row  (${batchMs.toFixed(0)} ms)`);
console.log(`speed-up        : ${(single / batchMs).toFixed(1)}x`);
model.dispose();

// Deno smoke test for feelc using the edge-style explicit `wasmBinary` path. Run:
//   deno run --allow-read packages/engine/scripts/deno-smoke.mjs
// Requires a built package (`make wasm && npm run build -w feelc`).
import { createEngine } from "../dist/index.js";

const wasmBinary = await Deno.readFile(new URL("../wasm/feelc.wasm", import.meta.url));
const engine = await createEngine({ wasmBinary });

const src = `model "m" {}
input n : number
decision cube : number = power(n, 3)`;
const cube = engine.run(src, "cube", { n: 4 }).output;
if (cube !== 64) {
  console.error("deno smoke FAIL: power(4,3) =", cube);
  Deno.exit(1);
}
console.log("deno smoke OK: power(4,3) =", cube);

// Side-effect import: wasm_exec.js defines globalThis.Go. It is version-matched to feelc.wasm
// (both produced by `make wasm`), so it must never be swapped for a hand-maintained copy.
import "../wasm/wasm_exec.js";

/** Construct a fresh Go runtime host (from the glue loaded above). */
export function newGo(): GoInstance {
  return new Go();
}

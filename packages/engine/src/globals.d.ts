// Ambient declarations for Go's WebAssembly runtime glue.
//
// `wasm_exec.js` (shipped in ../wasm, version-matched to feelc.wasm) is an IIFE that defines
// `globalThis.Go`. We import it for its side effect, so TS needs (a) to accept that import and
// (b) a type for the global `Go` constructor.

declare module "*wasm_exec.js";

interface GoInstance {
  argv: string[];
  env: Record<string, string>;
  importObject: WebAssembly.Imports;
  exit: (code: number) => void;
  run(instance: WebAssembly.Instance): Promise<void>;
}

// eslint-disable-next-line no-var
declare var Go: { new (): GoInstance };

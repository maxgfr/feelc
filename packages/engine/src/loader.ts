import { newGo } from "./runtime.js";
import type { CreateEngineOptions } from "./types.js";

/** The raw WASM surface: every method takes and returns a JSON string (or `{error}` on failure). */
export interface FeelcWasm {
  ready: boolean;
  // source-based (mirror the HTTP service)
  verify(json: string): string;
  run(json: string): string;
  graph(json: string): string;
  trace(json: string): string;
  model(json: string): string;
  required(json: string): string;
  check(json: string): string;
  // compiled-model handle path
  compile(source: string): string;
  load(json: string): string;
  export(json: string): string;
  evalCompiled(json: string): string;
  evalCompiledBatch(json: string): string;
  infoCompiled(json: string): string;
  requiredCompiled(json: string): string;
  dispose(json: string): string;
}

// Detect Node without relying on @types/node (keeps the package types environment-agnostic).
const nodeProcess = (globalThis as { process?: { versions?: { node?: string } } }).process;
const isNode = typeof nodeProcess?.versions?.node === "string";

function isBufferSource(v: unknown): v is ArrayBuffer | ArrayBufferView {
  return v instanceof ArrayBuffer || ArrayBuffer.isView(v);
}

/** Instantiate feelc.wasm against the Go import object, resolving the binary per environment. */
async function instantiate(
  importObject: WebAssembly.Imports,
  opts: CreateEngineOptions,
): Promise<WebAssembly.Instance> {
  const bin = opts.wasmBinary;

  // 1) An already-compiled module.
  if (bin instanceof WebAssembly.Module) {
    return WebAssembly.instantiate(bin, importObject);
  }
  // 2) Raw bytes.
  if (isBufferSource(bin)) {
    return (await WebAssembly.instantiate(bin as BufferSource, importObject)).instance;
  }
  // 3) A fetch Response (or a promise of one) — edge/custom hosting.
  if (bin) {
    const resp = await (bin as Promise<Response>);
    return (await WebAssembly.instantiateStreaming(resp, importObject)).instance;
  }

  // 4) Resolve a URL (explicit, or the .wasm shipped beside this module).
  const url = opts.wasmUrl ?? new URL("../wasm/feelc.wasm", import.meta.url);
  const href = url instanceof URL ? url.href : String(url);

  // Node: read from disk (file URLs and bare paths). Specifiers are computed so browser/edge
  // bundlers never try to include node: builtins.
  if (isNode && (href.startsWith("file:") || !/^https?:/.test(href))) {
    // Computed specifiers + local types: no @types/node, and browser/edge bundlers never include
    // node: builtins (the import is unreachable unless isNode).
    const fsMod = "node:fs/promises";
    const urlMod = "node:url";
    const { readFile } = (await import(fsMod)) as { readFile(p: string): Promise<Uint8Array> };
    const { fileURLToPath } = (await import(urlMod)) as { fileURLToPath(u: string): string };
    const path = href.startsWith("file:") ? fileURLToPath(href) : href;
    const bytes = await readFile(path);
    return (await WebAssembly.instantiate(bytes as BufferSource, importObject)).instance;
  }

  // Browser / edge: stream when possible, fall back to arrayBuffer (servers without the wasm MIME).
  const resp = await fetch(url as string | URL);
  try {
    return (await WebAssembly.instantiateStreaming(resp.clone(), importObject)).instance;
  } catch {
    const bytes = await resp.arrayBuffer();
    return (await WebAssembly.instantiate(bytes, importObject)).instance;
  }
}

function waitFor<T>(pred: () => T | undefined, timeoutMs = 10_000): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const start = Date.now();
    const tick = () => {
      const v = pred();
      if (v !== undefined) {
        resolve(v);
        return;
      }
      if (Date.now() - start > timeoutMs) {
        reject(new Error("feelc: engine did not become ready in time"));
        return;
      }
      setTimeout(tick, 5);
    };
    tick();
  });
}

/** Boot the WASM engine and return its (typed) function surface. */
export async function loadEngine(opts: CreateEngineOptions): Promise<FeelcWasm> {
  const token = opts.instanceToken || "feelc";
  const g = globalThis as Record<string, unknown>;
  // Tell Go which global name to register under (read synchronously inside main()).
  g["feelcInstanceToken"] = token;

  const go = newGo();
  const instance = await instantiate(go.importObject, opts);
  // go.run starts main(); main() registers globalThis[token] then parks on select{} forever — so it
  // never resolves. Do NOT await it.
  void go.run(instance);

  const feelc = await waitFor(() => {
    const obj = g[token] as (FeelcWasm & { ready?: boolean }) | undefined;
    return obj?.ready ? obj : undefined;
  });
  return feelc;
}

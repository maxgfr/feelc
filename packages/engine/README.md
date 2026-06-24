# @feelc/engine

The **portable feelc engine** — the real Go decision/calculation engine compiled to WebAssembly,
runnable directly in your TypeScript app with **no HTTP API**. Results are byte-for-byte identical to
the `feelc` CLI (same parser, same exact-decimal VM, same determinism).

Runs in the **browser**, **Node 18+**, **bundlers** (Vite / webpack 5 / Next / esbuild), and **edge
runtimes** (Cloudflare Workers, Deno). ESM-only.

```bash
npm install @feelc/engine
```

## Quick start

```ts
import { createEngine } from "@feelc/engine";

const feelc = await createEngine(); // loads the WASM once

const source = `
model "promo" {}
input cart_total : number >= 0
input is_member  : boolean
decision discount_pct : number {
  needs: cart_total, is_member
  hit: collect max
  >= 50  | -    => 5
  >= 100 | -    => 10
  -      | true => 8
}`;

const { output } = feelc.run(source, "discount_pct", { cart_total: 120, is_member: true });
console.log(output); // 10
```

## Three ways to run

The engine fits three workflows; pick per use case.

### 1. Compile at runtime (one-shot)

Simplest. Ships the `.rules` text and compiles on each call.

```ts
feelc.run(source, "discount_pct", { cart_total: 120, is_member: true });
feelc.verify(source);          // { hash, report, blockers }
feelc.model(source);           // { name, inputs, decisions }
feelc.required(source, "discount_pct");
```

### 2. Compile once, evaluate many (reactive / "ultra-opti")

Compile (or load) **once**, then evaluate repeatedly with no recompilation — ideal for live UIs.

```ts
const model = feelc.compile(source);

for (const cart_total of [40, 60, 120]) {
  console.log(model.evaluate("discount_pct", { cart_total, is_member: true }).output);
}

model.info();              // model surface for building forms
model.required("discount_pct");
model.dispose();           // free the WASM-side handle when done
```

### 3. Ship a precompiled artifact (`.ir.bin`)

Compile ahead of time, ship the binary, and `load` it in the client (smaller, hides rule source).
The bytes are interchangeable with the native CLI's `feelc compile`.

```ts
// build step (Node) — or: `npx feelc-compile rules.rules -o model.ir.bin`
import { writeFileSync } from "node:fs";
const bytes = feelc.compile(source).export();
writeFileSync("model.ir.bin", bytes);
```

```ts
// in the client
const bytes = await (await fetch("/model.ir.bin")).arrayBuffer();
const model = feelc.load(bytes);
model.evaluate("discount_pct", { cart_total: 120, is_member: true });
```

## Per-environment notes

- **Vite / webpack 5 / Next / esbuild** — works out of the box; the `.wasm` is resolved via
  `new URL("…", import.meta.url)` and emitted as an asset by your bundler. Calling
  `await createEngine()` at the top level needs a modern build target (Vite: `build.target: "esnext"`),
  or wrap it in an `async` function.
- **Node** — the `.wasm` is read from the package via `node:fs`. Nothing to configure.
- **Edge (Cloudflare Workers / Deno)** — import the `.wasm` as a module and pass it in:

  ```ts
  import wasm from "@feelc/engine/wasm/feelc.wasm"; // Worker: a WebAssembly.Module
  const feelc = await createEngine({ wasmBinary: wasm });
  ```

  Any environment can override resolution with `createEngine({ wasmUrl })` or `{ wasmBinary }`
  (bytes / `WebAssembly.Module` / a `fetch` `Response`).

- **Multiple instances** — one engine per realm by default. Pass `instanceToken` for isolated
  instances, or run one engine per Web Worker (the recommended way to scale / offload the main thread).

## Errors

Engine failures throw `FeelcError`. Compile errors carry a structured `diag` (`file`/`line`/`col`/`code`):

```ts
import { FeelcError } from "@feelc/engine";
try {
  feelc.run("model x {", "d", {});
} catch (e) {
  if (e instanceof FeelcError) console.error(e.message, e.diag);
}
```

## Caveats

- **Decimal precision.** Exact decimals cross back into JS as `number` (float64); very large/precise
  values can lose precision. (The engine itself stays exact.)
- **Bundle size.** The `.wasm` is ~6 MB (~1.5 MB gzipped), loaded lazily on `createEngine()`.

## Releasing (maintainers)

```bash
make wasm                                   # build feelc.wasm + wasm_exec.js (from the Go source)
npm ci && npm -w @feelc/engine run build && npm -w @feelc/engine test
cd packages/engine && npm publish --access public   # first release: bootstrap on npm
```

After the first publish, enable **npm OIDC trusted publishing** for `@feelc/engine`; subsequent
releases are published by CI on a `v*` tag (`.github/workflows/npm.yml`) with no token.

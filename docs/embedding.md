# Embed in your app (`@feelc/engine`)

Run the **real** feelc engine directly in your TypeScript app — **no HTTP API**. `@feelc/engine` is the
Go engine compiled to WebAssembly, so results are byte-for-byte identical to the `feelc` CLI (same
parser, same exact-decimal VM, same determinism). It runs in the **browser**, **Node 18+**, **bundlers**
(Vite / webpack 5 / Next / esbuild), and **edge runtimes** (Cloudflare Workers, Deno). ESM-only.

This is the embeddable counterpart to the [in-browser playground](../playground/): same WASM engine,
packaged for your own apps. The `.wasm` is ~6 MB (~1.5 MB gzipped), loaded lazily on `createEngine()`.

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

| Mode | API | When |
|------|-----|------|
| Compile at runtime | `feelc.run(source, decision, input)` | One-shot; you have the `.rules` text. |
| Compile once, evaluate many | `feelc.compile(source)` → `model.evaluate(...)` | Reactive UIs; avoid recompiling per keystroke. |
| Ship a precompiled artifact | `feelc.load(bytes)` → `model.evaluate(...)` | Smallest payload; hides rule source. |

```ts
// compile once, evaluate many
const model = feelc.compile(source);
model.evaluate("discount_pct", { cart_total: 120, is_member: true }); // no recompile
model.info();              // { name, inputs, decisions } — build forms from this
model.required("discount_pct");
model.dispose();           // free the WASM-side handle when done
```

The precompiled `.ir.bin` artifact is **interchangeable with the CLI**: `feelc compile rules.rules -o
model.ir.bin` produces bytes that `feelc.load()` accepts, and `model.export()` produces bytes the CLI
runs (see the [IR format](ir-format.html)). You can also produce one from JS with the bundled
`feelc-compile` CLI:

```bash
npx feelc-compile rules.rules -o model.ir.bin
```

```ts
const bytes = await (await fetch("/model.ir.bin")).arrayBuffer();
const model = feelc.load(bytes);
model.evaluate("discount_pct", { cart_total: 120, is_member: true });
```

## Full surface

Source-based (mirror the HTTP service): `run`, `verify`, `model`, `graph`, `trace`, `required`, `check`.
Compiled-model: `compile`, `load`, then on the model `evaluate`, `info`, `required`, `export`, `dispose`.

```ts
feelc.verify(source);            // { hash, report, blockers }
feelc.model(source);             // { name, inputs, decisions }
feelc.graph(source);             // { mermaid, dot, graph, findings, blockers }
feelc.required(source, "discount_pct");
feelc.check(source, [{ decision: "discount_pct", input: { cart_total: 120, is_member: true }, expect: 10 }]);
```

## Per-environment notes

- **Vite / webpack 5 / Next / esbuild** — works out of the box; the `.wasm` is resolved via
  `new URL("…", import.meta.url)` and emitted as an asset by your bundler. If you call
  `await createEngine()` at the top level of a module, set a modern build target (Vite:
  `build.target: "esnext"`) so top-level `await` is allowed — or wrap the call in an `async` function.
- **Node** — the `.wasm` is read from the package via `node:fs`. Nothing to configure.
- **Edge (Cloudflare Workers / Deno)** — import the `.wasm` as a module and pass it in:

  ```ts
  import wasm from "@feelc/engine/wasm/feelc.wasm"; // a WebAssembly.Module
  const feelc = await createEngine({ wasmBinary: wasm });
  ```

  Any environment can override resolution with `createEngine({ wasmUrl })` or `{ wasmBinary }`.
- **Multiple instances / threads** — one engine per realm by default; pass `instanceToken` to isolate
  instances, or run one engine per Web Worker to keep evaluation off the main thread.

## Errors

Engine failures throw `FeelcError`; compile errors carry a structured diagnostic (`file`/`line`/`col`/
`code`), the same [error schema](error-schema.html) the CLI and HTTP API use.

```ts
import { FeelcError } from "@feelc/engine";
try {
  feelc.run("model x {", "d", {});
} catch (e) {
  if (e instanceof FeelcError) console.error(e.message, e.diag);
}
```

## Caveat — decimal precision

Outputs cross into JS as `number` (float64), so very large/precise decimals can lose precision in JS.
The engine itself stays exact (`apd` decimals); this only affects the value once it is handed back to
JavaScript.

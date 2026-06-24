// Cloudflare Worker (also adaptable to Deno) embedding the feelc engine on the edge.
//
// Edge runtimes have no node:fs and don't resolve `new URL(..., import.meta.url)` file assets, so we
// import the .wasm as a module (the runtime hands us a WebAssembly.Module) and pass it through the
// `wasmBinary` override. Everything else is identical to the browser/Node API.
import { createEngine, type FeelcEngine } from "@feelc/engine";
import wasm from "@feelc/engine/wasm/feelc.wasm"; // Cloudflare compiles this to a WebAssembly.Module

const SOURCE = `model "promo" {}
input cart_total : number >= 0
input is_member  : boolean
decision discount_pct : number {
  needs: cart_total, is_member
  hit: collect max
  >= 50  | -    => 5
  >= 100 | -    => 10
  -      | true => 8
}`;

let enginePromise: Promise<FeelcEngine> | undefined;
function engine(): Promise<FeelcEngine> {
  // Boot once per isolate; reuse across requests.
  enginePromise ??= createEngine({ wasmBinary: wasm as WebAssembly.Module });
  return enginePromise;
}

export default {
  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const feelc = await engine();
    const result = feelc.run(SOURCE, "discount_pct", {
      cart_total: Number(url.searchParams.get("cart_total") ?? 0),
      is_member: url.searchParams.get("is_member") === "true",
    });
    return Response.json(result);
  },
};

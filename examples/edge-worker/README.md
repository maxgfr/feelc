# feelc on the edge (Cloudflare Workers)

Embeds `feelc` in a Cloudflare Worker. Edge runtimes have no `node:fs` and don't resolve
`new URL(..., import.meta.url)` file assets, so the `.wasm` is imported as a module and passed via the
`wasmBinary` override — see `src/worker.ts`.

```bash
# from the repo root, after `npm install` and `npm -w feelc run build`
npx wrangler dev examples/edge-worker/src/worker.ts

# then:
curl 'http://localhost:8787/?cart_total=120&is_member=true'
# => {"decision":"discount_pct","output":10}
```

The same pattern works in Deno (`import wasm from "feelc/wasm/feelc.wasm"` then
`createEngine({ wasmBinary: wasm })`).

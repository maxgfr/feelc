# Environment matrix

feelc ships as **one cgo-free Go binary** plus a **portable WASM** build (`feelc`). The same
engine — byte-for-byte identical results — runs across every target below. This page records what is
validated and how to reproduce each check.

| Environment | What runs | How it's validated | Status |
|---|---|---|---|
| **Go (native)** | the engine + CLI, amd64 + arm64 | `make test` (+ goldens replayed on both arches in CI); `go test -race ./...` | ✅ |
| **Node ≥ 18** | `feelc` (WASM) | `npm run test -w feelc` (233 tests) + `node-smoke` parity (WASM == native CLI, 225 cases) | ✅ |
| **Bun** | `feelc` (WASM) | `node-smoke` parity under Bun; `bench-batch.mjs` | ✅ |
| **Vite (browser bundler)** | `feelc` (WASM) | `npm run build -w @feelc-examples/browser-vite` (bundler resolves the `.wasm` asset) | ✅ |
| **Deno** | `feelc` (WASM, edge path) | `createEngine({ wasmBinary: await Deno.readFile(...) })` then evaluate | ✅ |
| **Cloudflare Workers / edge** | `feelc` (WASM, edge path) | `examples/edge-worker` — `import wasm from "feelc/wasm/feelc.wasm"` + `createEngine({ wasmBinary })` | ✅ (example) |
| **Docker** | the CLI as an HTTP service | `docker build .` → distroless image; `/readyz` → 200, `feelc healthcheck`, `serve` smoke | ✅ |

## Reproduce

```bash
# Go (native) — bit-for-bit deterministic across arches
make test

# Node + Bun (WASM == native CLI parity)
make wasm && npm run build -w feelc
npm run test -w feelc
npm run test -w @feelc-examples/node-smoke          # node
(cd examples/node-smoke && bun run test)            # bun

# Vite browser bundle
npm run build -w @feelc-examples/browser-vite

# Deno (edge-style wasmBinary)
deno run --allow-read packages/engine/scripts/deno-smoke.mjs

# Docker (service + health)
docker build -t feelc .
docker run --rm -p 8080:8080 -v "$PWD/sample-project:/work" feelc
curl -fsS localhost:8080/readyz                     # 200 when ready
```

Or run the lot with `scripts/env-matrix.sh` (skips Docker by default; `WITH_DOCKER=1` to include it).

## Edge / Deno notes

Bundler and Node targets locate `feelc.wasm` via `import.meta.url`. Runtimes without that resolution
(Cloudflare Workers, Deno) pass the bytes explicitly:

```ts
import { createEngine } from "feelc";
// Deno:    const wasmBinary = await Deno.readFile(new URL("...feelc.wasm", import.meta.url));
// Workers: import wasmBinary from "feelc/wasm/feelc.wasm";
const engine = await createEngine({ wasmBinary });
```

The package is **ESM-only**; in a bundler that inlines top-level `await`, use a modern target
(Vite: `build.target: "esnext"`).

import { defineConfig } from "tsup";

// ESM-only on purpose: every target we support (browser, Vite/webpack/Next, Node 18+, edge) is
// ESM, and `import.meta.url` (used to locate feelc.wasm) has no clean CJS equivalent.
//
// platform "neutral" keeps `import.meta.url` and the `new URL(..., import.meta.url)` wasm reference
// intact so the CONSUMER's bundler resolves the asset. The ".wasm": "copy" loader makes esbuild copy
// feelc.wasm next to dist/ and rewrite that reference during our own build.
export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm"],
  target: "es2022",
  platform: "neutral",
  dts: true,
  sourcemap: true,
  clean: true,
  minify: false,
  esbuildOptions(options) {
    options.loader = { ...options.loader, ".wasm": "copy" };
  },
});

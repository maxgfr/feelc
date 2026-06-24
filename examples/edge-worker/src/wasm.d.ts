// Edge runtimes (Cloudflare Workers) compile a .wasm import to a WebAssembly.Module.
declare module "*.wasm" {
  const module: WebAssembly.Module;
  export default module;
}

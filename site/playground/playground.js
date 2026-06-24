"use strict";
// feelc playground (host) — the REAL Go engine runs in your browser via WebAssembly. There is no
// server and no LLM: every result (verify, graph, run, trace) comes from the deterministic engine,
// identical to the `feelc` CLI. The renderers + reactive simulator live in shared.js (the single
// source of truth shared with the embedded `serve --ui`); build.mjs copies it next to this file. This
// host only provides the WASM transport (the `api()` shim), the example picker and the default model,
// then hands off to shared.js's initSim().

// ---- WASM bootstrap ----
async function boot() {
  const status = $("wasm-status");
  try {
    const go = new Go(); // from wasm_exec.js
    const resp = await fetch("../static/feelc.wasm");
    const bytes = await resp.arrayBuffer(); // arrayBuffer (not streaming) so local servers without the wasm MIME work
    const result = await WebAssembly.instantiate(bytes, go.importObject);
    go.run(result.instance); // starts main(): registers window.feelc, then blocks on select{}
    await waitFor(() => window.feelc && window.feelc.ready);
    status.textContent = "engine ready · runs in your browser";
    status.classList.add("ok");
    onReady();
  } catch (err) {
    status.textContent = "failed to load engine: " + err.message;
    status.classList.add("err");
  }
}
function waitFor(pred) {
  return new Promise((resolve) => {
    const tick = () => (pred() ? resolve() : setTimeout(tick, 20));
    tick();
  });
}

// ---- WASM transport: same signature as the serve --ui HTTP `api()`, so shared.js is reused unchanged.
// The WASM methods take/return JSON strings; method/headers in opts are ignored.
async function api(path, opts) {
  const body = (opts && opts.body) || "";
  const fn = {
    "/v1/verify": "verify",
    "/v1/graph": "graph",
    "/v1/run": "run",
    "/v1/trace": "trace",
    "/v1/required": "required",
    "/v1/check": "check",
    "/v1/model": "model",
  }[path];
  if (!fn || !window.feelc || !window.feelc[fn]) {
    return { ok: false, status: 404, data: { error: "no such engine method: " + path } };
  }
  let data = null;
  try {
    data = JSON.parse(window.feelc[fn](body));
  } catch (e) {
    return { ok: false, status: 500, data: { error: String(e) } };
  }
  // compile/eval failures are reported as {error}; mirror the HTTP 422.
  if (data && data.error !== undefined) return { ok: false, status: 422, data };
  return { ok: true, status: 200, data };
}

// ---- examples ----
async function loadExamples() {
  try {
    const list = await (await fetch("../examples.json")).json();
    window.__examples = list;
    const sel = $("example");
    list.forEach((ex, i) => {
      const o = document.createElement("option");
      o.value = String(i);
      o.textContent = ex.title || ex.name;
      sel.appendChild(o);
    });
    if (list.length) { sel.value = "0"; selectExample(0); }
  } catch (_) { /* examples.json absent in local dev: fall back to the inline default below */ }
}
function selectExample(i) {
  const ex = window.__examples && window.__examples[i];
  if (!ex) return;
  $("source").value = ex.rules;
  refreshAll(); // shared.js: re-verify, re-graph, rebuild the reactive simulator
}

const DEFAULT_RULES = `model "promo" {}

input cart_total : number >= 0
input is_member  : boolean

# Keep the BEST applicable discount (collect max).
decision discount_pct : number {
  needs: cart_total, is_member
  hit: collect max
  >= 50  | -    => 5
  >= 100 | -    => 10
  -      | true => 8
}`;

// ---- wiring ----
function onReady() {
  $("verify-btn").addEventListener("click", verify);
  $("graph-btn").addEventListener("click", showGraph);
  $("trace-btn").addEventListener("click", showTrace);
  $("example").addEventListener("change", (e) => { if (e.target.value !== "") selectExample(Number(e.target.value)); });
  initSim(); // shared.js: reactive inputs ⇄ JSON ⇄ result, live on every edit
  loadExamples().then(() => {
    if (!$("source").value.trim()) { $("source").value = DEFAULT_RULES; refreshAll(); }
  });
}

boot();

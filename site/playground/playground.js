"use strict";
// feelc playground — the REAL Go engine runs in your browser via WebAssembly. There is no server
// and no LLM: every result (verify, graph, run, traceability) comes from the deterministic engine,
// identical to the `feelc` CLI. The verify/graph/trace/run renderers are shared with the embedded
// `serve --ui` app; only the transport differs (WASM calls instead of HTTP fetch).

const $ = (id) => document.getElementById(id);

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

// ---- WASM dispatcher: same signature as the HTTP `api()`, so the renderers are reused unchanged ----
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

// ---- helpers reused from the serve UI ----
function escapeHtml(s) {
  return String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}

// ---- verify ----
function clearReport() { $("report").innerHTML = ""; }
function banner(cls, text) {
  const d = document.createElement("div");
  d.className = `banner ${cls}`;
  d.textContent = text;
  $("report").appendChild(d);
}
function renderFinding(f) {
  const d = document.createElement("div");
  d.className = `finding ${f.severity}`;
  d.innerHTML = `<span class="k">${escapeHtml(f.kind)}</span>${escapeHtml(f.decision || "")} — ${escapeHtml(f.message)}`;
  if (f.witness) {
    const w = document.createElement("div");
    w.className = "witness";
    w.textContent = "counter-example: " + JSON.stringify(f.witness);
    d.appendChild(w);
  }
  $("report").appendChild(d);
}
async function verify() {
  const src = $("source").value;
  if (!src.trim()) { clearReport(); return; }
  clearReport();
  const { ok, status, data } = await api("/v1/verify", { body: src });
  if (!ok) {
    const e = data && data.error;
    banner("err", e && e.message ? `compile error${e.line ? " (line " + e.line + ")" : ""}: ${e.message}` : `verify failed (${status})`);
    return;
  }
  const findings = (data.report && data.report.findings) || [];
  if (data.blockers === 0 && findings.length === 0) {
    banner("ok", "✓ verified: complete and consistent (no findings)");
  } else {
    banner(data.blockers > 0 ? "err" : "ok", `${data.blockers} blocker(s), ${findings.length} finding(s)`);
    findings.forEach(renderFinding);
  }
}

// ---- graph (DRG): zero-dependency layered SVG ----
const SVGNS = "http://www.w3.org/2000/svg";
function svgEl(name, attrs) {
  const el = document.createElementNS(SVGNS, name);
  for (const k in attrs) el.setAttribute(k, attrs[k]);
  return el;
}
async function showGraph() {
  const src = $("source").value;
  const g = $("graph");
  g.innerHTML = "";
  if (!src.trim()) return;
  const { ok, status, data } = await api("/v1/graph", { body: src });
  if (!ok) {
    const e = data && data.error;
    g.textContent = e && e.message ? `compile error${e.line ? " (line " + e.line + ")" : ""}: ${e.message}` : `graph failed (${status})`;
    return;
  }
  renderGraph(g, data.graph);
}
function renderGraph(container, graph) {
  const nodes = graph.nodes || [], edges = graph.edges || [];
  const rank = {};
  nodes.forEach((n) => { rank[n.id] = 0; });
  for (let pass = 0; pass <= nodes.length; pass++) {
    let changed = false;
    edges.forEach((e) => {
      const r = (rank[e.from] || 0) + 1;
      if (r > (rank[e.into] || 0)) { rank[e.into] = r; changed = true; }
    });
    if (!changed) break;
  }
  const cols = {};
  nodes.forEach((n) => { const r = rank[n.id] || 0; (cols[r] = cols[r] || []).push(n); });
  const colW = 210, rowH = 64, nodeW = 160, nodeH = 38, padX = 16, padY = 16;
  const pos = {};
  let maxRows = 0;
  const rankKeys = Object.keys(cols).map(Number).sort((a, b) => a - b);
  rankKeys.forEach((r) => {
    cols[r].forEach((n, i) => { pos[n.id] = { x: padX + r * colW, y: padY + i * rowH }; });
    maxRows = Math.max(maxRows, cols[r].length);
  });
  const W = padX * 2 + (rankKeys.length - 1) * colW + nodeW;
  const H = padY * 2 + Math.max(1, maxRows) * rowH;
  const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, width: "100%", height: String(Math.min(H, 520)) });
  const defs = svgEl("defs", {});
  const marker = svgEl("marker", { id: "arrow", viewBox: "0 0 10 10", refX: "9", refY: "5", markerWidth: "7", markerHeight: "7", orient: "auto-start-reverse" });
  marker.appendChild(svgEl("path", { d: "M0,0 L10,5 L0,10 z", fill: "#6b7785" }));
  defs.appendChild(marker);
  svg.appendChild(defs);
  edges.forEach((e) => {
    const a = pos[e.from], b = pos[e.into];
    if (!a || !b) return;
    const x1 = a.x + nodeW, y1 = a.y + nodeH / 2, x2 = b.x, y2 = b.y + nodeH / 2, mx = (x1 + x2) / 2;
    svg.appendChild(svgEl("path", { d: `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`, fill: "none", stroke: "#6b7785", "stroke-width": "1.5", "marker-end": "url(#arrow)" }));
  });
  const sevFill = { error: "#e06b6b", warning: "#d9a441", info: "#7fb3ff" };
  nodes.forEach((n) => {
    const p = pos[n.id];
    if (!p) return;
    const fill = sevFill[n.severity] || (n.kind === "input" ? "#243240" : "#1f6feb");
    const rect = svgEl("rect", { x: p.x, y: p.y, width: nodeW, height: nodeH, rx: n.kind === "input" ? 18 : 6, fill, stroke: "#2a3340" });
    if (n.findings && n.findings.length) { const t = svgEl("title", {}); t.textContent = n.findings.join("\n"); rect.appendChild(t); }
    svg.appendChild(rect);
    let label = n.name;
    if (n.kind === "input" && n.type) label += " : " + n.type;
    else if (n.hitPolicy) label += " [" + n.hitPolicy + "]";
    const txt = svgEl("text", { x: p.x + nodeW / 2, y: p.y + nodeH / 2 + 4, "text-anchor": "middle", fill: "#fff", "font-size": "12" });
    txt.textContent = label.length > 22 ? label.slice(0, 21) + "…" : label;
    svg.appendChild(txt);
  });
  container.appendChild(svg);
}

// ---- traceability ----
async function showTrace() {
  const src = $("source").value;
  const t = $("trace");
  t.innerHTML = "";
  if (!src.trim()) return;
  const { ok, status, data } = await api("/v1/trace", { body: JSON.stringify({ rules: src, spec: "" }) });
  if (!ok) {
    const e = data && data.error;
    t.textContent = e && e.message ? `compile error: ${e.message}` : `traceability failed (${status})`;
    return;
  }
  const cov = data.coverage || {};
  const head = document.createElement("div");
  head.className = "headline";
  head.textContent = `${cov.decisionsSourced || 0}/${cov.decisions || 0} decisions cite a @source`;
  t.appendChild(head);
  if ((data.untraced || []).length) {
    const u = document.createElement("div");
    u.className = "finding warning";
    u.innerHTML = `<span class="k">untraced</span>${escapeHtml(data.untraced.join(", "))} — no @source`;
    t.appendChild(u);
  }
  (data.decisions || []).filter((d) => d.source).forEach((d) => {
    const row = document.createElement("div");
    row.className = "span covered";
    row.innerHTML = `<span class="mark">✓</span><span class="txt">${escapeHtml(d.decision)}</span><span class="by">${escapeHtml(d.source)}</span>`;
    t.appendChild(row);
  });
}

// ---- simulator (question-flow) ----
async function buildForm() {
  const decision = $("decision").value.trim();
  const form = $("sim-form");
  form.innerHTML = "";
  if (!decision) { form.textContent = "Enter a decision name first."; return; }
  const { ok, status, data } = await api("/v1/required", { body: JSON.stringify({ rules: $("source").value, decision }) });
  if (!ok) {
    const e = data && data.error;
    form.textContent = e && e.message ? `error: ${e.message}` : `cannot build form (${status})`;
    return;
  }
  (data.inputs || []).forEach((inp) => form.appendChild(widget(inp)));
  const compute = document.createElement("button");
  compute.textContent = "Compute";
  compute.className = "primary";
  compute.addEventListener("click", () => computeForm(decision));
  form.appendChild(compute);
}
function widget(inp) {
  const wrap = document.createElement("label");
  wrap.className = "field";
  const cap = document.createElement("span");
  cap.textContent = (inp.question || inp.title || inp.name) + (inp.domain ? " (" + inp.domain + ")" : "");
  wrap.appendChild(cap);
  let el;
  const enumVals = parseEnum(inp.domain);
  if (inp.type === "boolean") {
    el = document.createElement("input"); el.type = "checkbox";
  } else if (enumVals) {
    el = document.createElement("select");
    enumVals.forEach((v) => { const o = document.createElement("option"); o.value = v; o.textContent = v; el.appendChild(o); });
  } else if (inp.type === "number") {
    el = document.createElement("input"); el.type = "number"; el.step = "any";
    const rng = parseRange(inp.domain);
    if (rng) { if (rng.min != null) el.min = rng.min; if (rng.max != null) el.max = rng.max; }
  } else {
    el = document.createElement("input"); el.type = "text";
  }
  el.dataset.name = inp.name;
  el.dataset.itype = inp.type;
  wrap.appendChild(el);
  return wrap;
}
function parseEnum(domain) {
  const m = (domain || "").match(/in\s*\{(.+)\}/);
  if (!m) return null;
  return m[1].split(",").map((s) => s.trim().replace(/^"|"$/g, ""));
}
function parseRange(domain) {
  const m = (domain || "").match(/in\s*[\[(]\s*(-?[\d.]+|-inf)\s*\.\.\s*(-?[\d.]+|\+inf)\s*[\])]/);
  if (!m) return null;
  return { min: m[1] === "-inf" ? null : m[1], max: m[2] === "+inf" ? null : m[2] };
}
async function computeForm(decision) {
  const input = {};
  $("sim-form").querySelectorAll("[data-name]").forEach((el) => {
    const name = el.dataset.name, t = el.dataset.itype;
    if (t === "boolean") input[name] = el.checked;
    else if (t === "number") { if (el.value !== "") input[name] = Number(el.value); }
    else if (el.value !== "") input[name] = el.value;
  });
  $("input").value = JSON.stringify(input);
  await runDecision(decision, input);
}

// ---- run ----
function detectDecision() {
  if ($("decision").value.trim()) return;
  const m = $("source").value.match(/decision\s+([A-Za-z_][\w]*)/);
  if (m) $("decision").value = m[1];
}
async function run() {
  const decision = $("decision").value.trim();
  if (!decision) { $("output").textContent = "Enter a decision name."; return; }
  let input = {};
  const raw = $("input").value.trim();
  if (raw) {
    try { input = JSON.parse(raw); } catch (e) { $("output").textContent = "Invalid JSON input: " + e.message; return; }
  }
  await runDecision(decision, input);
}
async function runDecision(decision, input) {
  const { ok, status, data } = await api("/v1/run", { body: JSON.stringify({ rules: $("source").value, decision, input, explain: true }) });
  if (!ok) {
    const e = data && data.error;
    $("output").textContent = (e && e.message) ? `error: ${e.message}` : `run failed (${status})`;
    return;
  }
  let txt = "→ " + JSON.stringify(data.output);
  if (data.trace) txt += "\n\ntrace:\n" + JSON.stringify(data.trace, null, 2);
  $("output").textContent = txt;
}

// ---- live auto-interpretation: verify + graph + auto-run the goal decision on every edit ----
let liveTimer = null;
function live() {
  clearTimeout(liveTimer);
  liveTimer = setTimeout(runLive, 350);
}
async function runLive() {
  detectDecision();
  await verify();
  await showGraph();
  await autoRun();
}
async function autoRun() {
  const src = $("source").value;
  if (!src.trim()) return;
  const m = await api("/v1/model", { body: src });
  if (!m.ok || !m.data.decisions || !m.data.decisions.length) return;
  const goal = m.data.decisions[m.data.decisions.length - 1].name; // last decision = the goal
  $("decision").value = goal;
  const req = await api("/v1/required", { body: JSON.stringify({ rules: src, decision: goal }) });
  if (!req.ok) return;
  const input = defaultInput(req.data.inputs || []);
  $("input").value = JSON.stringify(input);
  await runDecision(goal, input);
}
// defaultInput picks a plausible value per input from its declared domain (so the live demo runs
// without the user filling anything in).
function defaultInput(inputs) {
  const o = {};
  inputs.forEach((inp) => {
    if (inp.type === "boolean") { o[inp.name] = true; return; }
    if (inp.type === "date") { o[inp.name] = "2020-01-01"; return; }
    if (inp.type === "duration") { o[inp.name] = "P365D"; return; }
    if (inp.type === "number") {
      const r = parseRange(inp.domain);
      o[inp.name] = r && r.min != null ? Number(r.min) : (r && r.max != null ? Number(r.max) : 1);
      return;
    }
    const e = parseEnum(inp.domain);
    o[inp.name] = e ? e[0] : "";
  });
  return o;
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
  $("decision").value = "";
  $("sim-form").innerHTML = "";
  live();
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
  $("run-btn").addEventListener("click", run);
  $("form-btn").addEventListener("click", buildForm);
  $("source").addEventListener("input", live);
  $("example").addEventListener("change", (e) => { if (e.target.value !== "") selectExample(Number(e.target.value)); });
  loadExamples().then(() => {
    if (!$("source").value.trim()) { $("source").value = DEFAULT_RULES; live(); }
  });
}

boot();

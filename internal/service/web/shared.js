"use strict";
// feelc — shared, transport-agnostic renderers + the reactive simulator.
//
// This is the SINGLE source of truth for everything that turns engine results into UI, shared by BOTH
// front-ends so they can never drift again:
//   • the embedded `feelc serve --ui` (internal/service/web/app.js, transport = HTTP fetch), and
//   • the GitHub Pages playground (site/playground/playground.js, transport = WebAssembly).
// build.mjs copies this file verbatim into site/playground/ at site-build time.
//
// The only thing that differs between the two is the global `api(path, opts) -> {ok,status,data}`
// function, which each host defines (HTTP vs WASM). Every renderer here calls that global `api`.
// Host files also provide the LLM/chat/settings (serve --ui) or the WASM boot + examples (playground).

/* global api */

const $ = (id) => document.getElementById(id);

function escapeHtml(s) {
  return String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}
function debounce(fn, ms) {
  let t = null;
  return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
}

// ---- small POST helpers so callers don't repeat headers (WASM ignores method/headers) ----
const postText = (path, body) => api(path, { method: "POST", headers: { "Content-Type": "text/plain" }, body });
const postJSON = (path, obj) => api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(obj) });

// ====================================================================================
// verification report
// ====================================================================================
function clearReport() { const r = $("report"); if (r) r.innerHTML = ""; }
function banner(cls, text) {
  const r = $("report"); if (!r) return;
  const d = document.createElement("div");
  d.className = `banner ${cls}`;
  d.textContent = text;
  r.appendChild(d);
}
function renderFinding(f) {
  const r = $("report"); if (!r) return;
  const d = document.createElement("div");
  d.className = `finding ${f.severity}`;
  d.innerHTML = `<span class="k">${escapeHtml(f.kind)}</span>${escapeHtml(f.decision || "")} — ${escapeHtml(f.message)}`;
  if (f.witness) {
    const w = document.createElement("div");
    w.className = "witness";
    w.textContent = "counter-example: " + JSON.stringify(f.witness);
    d.appendChild(w);
  }
  r.appendChild(d);
}
async function verify() {
  const src = $("source").value;
  if (!src.trim()) { clearReport(); return; }
  clearReport();
  const { ok, status, data } = await postText("/v1/verify", src);
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

// ====================================================================================
// decision-requirements graph (zero-dependency layered SVG, interactive)
// ====================================================================================
const SVGNS = "http://www.w3.org/2000/svg";
function svgEl(name, attrs) {
  const el = document.createElementNS(SVGNS, name);
  for (const k in attrs) el.setAttribute(k, attrs[k]);
  return el;
}
async function showGraph() {
  const src = $("source").value;
  const g = $("graph");
  if (!g) return;
  g.innerHTML = "";
  if (!src.trim()) return;
  const { ok, status, data } = await postText("/v1/graph", src);
  if (!ok) {
    const e = data && data.error;
    g.textContent = e && e.message ? `compile error${e.line ? " (line " + e.line + ")" : ""}: ${e.message}` : `graph failed (${status})`;
    return;
  }
  renderGraph(g, data.graph);
}
// Per-module accent palette (project mode): each module gets a stable colour for node borders + legend.
const MOD_PALETTE = ["#6ea8fe", "#7fd1ae", "#e0a458", "#c08cf0", "#e88aa8", "#5ec8c8", "#b5c46b"];

// renderGraph lays the DRG out as a left-to-right layered SVG and makes it interactive: wheel-zoom +
// drag-pan + fit, a legend, click-a-node-to-inspect, module-coloured borders, dashed cross-module
// edges. Each node group carries data-name/data-kind so highlightGraph() can light the firing path
// after a run without re-rendering (preserving zoom/pan). Rank 0 = inputs.
function renderGraph(container, graph) {
  const nodes = graph.nodes || [], edges = graph.edges || [];
  container.innerHTML = "";
  if (!nodes.length) { container.textContent = "nothing to graph yet"; return; }

  const rank = {};
  nodes.forEach((n) => { rank[n.id] = 0; });
  for (let pass = 0; pass <= nodes.length; pass++) {
    let changed = false;
    edges.forEach((e) => { const r = (rank[e.from] || 0) + 1; if (r > (rank[e.into] || 0)) { rank[e.into] = r; changed = true; } });
    if (!changed) break;
  }

  const modules = [...new Set(nodes.map((n) => n.module).filter(Boolean))];
  const modColor = {};
  modules.forEach((m, i) => { modColor[m] = MOD_PALETTE[i % MOD_PALETTE.length]; });

  const cols = {};
  nodes.forEach((n) => { const r = rank[n.id] || 0; (cols[r] = cols[r] || []).push(n); });
  const colW = 220, rowH = 72, nodeW = 170, nodeH = 46, padX = 30, padY = 30;
  const pos = {};
  let maxRows = 0;
  const rankKeys = Object.keys(cols).map(Number).sort((a, b) => a - b);
  rankKeys.forEach((r) => {
    cols[r].sort((a, b) => (a.module || "").localeCompare(b.module || "") || a.name.localeCompare(b.name));
    cols[r].forEach((n, i) => { pos[n.id] = { x: padX + r * colW, y: padY + i * rowH }; });
    maxRows = Math.max(maxRows, cols[r].length);
  });
  const W = padX * 2 + (rankKeys.length - 1) * colW + nodeW;
  const H = padY * 2 + Math.max(1, maxRows) * rowH;

  const wrap = document.createElement("div");
  wrap.className = "graph-wrap";

  const controls = document.createElement("div");
  controls.className = "graph-controls";
  const mkBtn = (label, title, fn) => { const b = document.createElement("button"); b.type = "button"; b.textContent = label; b.title = title; b.onclick = fn; controls.appendChild(b); };

  const svg = svgEl("svg", { class: "drg", width: "100%", viewBox: `0 0 ${W} ${H}` });
  const defs = svgEl("defs", {});
  const mkMarker = (id, color) => { const m = svgEl("marker", { id, viewBox: "0 0 10 10", refX: "9", refY: "5", markerWidth: "7", markerHeight: "7", orient: "auto-start-reverse" }); m.appendChild(svgEl("path", { d: "M0,0 L10,5 L0,10 z", fill: color })); return m; };
  defs.appendChild(mkMarker("arrow", "#6b7785"));
  defs.appendChild(mkMarker("arrowx", "#c08cf0"));
  svg.appendChild(defs);

  edges.forEach((e) => {
    const a = pos[e.from], b = pos[e.into];
    if (!a || !b) return;
    const x1 = a.x + nodeW, y1 = a.y + nodeH / 2, x2 = b.x, y2 = b.y + nodeH / 2, mx = (x1 + x2) / 2;
    svg.appendChild(svgEl("path", {
      d: `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`, fill: "none",
      stroke: e.crossModule ? "#c08cf0" : "#6b7785", "stroke-width": e.crossModule ? "1.8" : "1.5",
      "stroke-dasharray": e.crossModule ? "5,4" : "0", "marker-end": e.crossModule ? "url(#arrowx)" : "url(#arrow)",
    }));
  });

  const inspect = document.createElement("div");
  inspect.className = "graph-inspect";
  inspect.hidden = true;
  const selectNode = (n) => {
    const rows = [`<b>${escapeHtml(n.local || n.name)}</b>`, `<span class="gi-kind">${n.kind}${n.decisionKind ? " · " + n.decisionKind : ""}</span>`];
    if (n.module) rows.push(`module: <code>${escapeHtml(n.module)}</code>`);
    if (n.type) rows.push(`type: ${escapeHtml(n.type)}`);
    if (n.hitPolicy) rows.push(`hit policy: ${escapeHtml(n.hitPolicy)}`);
    if (n.line) rows.push(`source line ${n.line}`);
    if (n.findings && n.findings.length) rows.push(`<div class="gi-find">${n.findings.map((f) => "• " + escapeHtml(f)).join("<br/>")}</div>`);
    inspect.innerHTML = `<button class="gi-close" type="button" title="close">×</button>` + rows.join("<br/>");
    inspect.querySelector(".gi-close").onclick = () => { inspect.hidden = true; };
    inspect.hidden = false;
  };

  const sevFill = { error: "#e06b6b", warning: "#d9a441", info: "#7fb3ff" };
  nodes.forEach((n) => {
    const p = pos[n.id];
    if (!p) return;
    const node = svgEl("g", { class: "gnode", transform: `translate(${p.x},${p.y})`, "data-name": n.name, "data-kind": n.kind });
    const fill = sevFill[n.severity] || (n.kind === "input" ? "#243240" : "#1f6feb");
    const stroke = n.module ? modColor[n.module] : "#2a3340";
    const rect = svgEl("rect", { x: 0, y: 0, width: nodeW, height: nodeH, rx: n.kind === "input" ? 22 : 8, fill, stroke, "stroke-width": n.module ? "2" : "1", class: "gn-rect" });
    if (n.findings && n.findings.length) { const t = svgEl("title", {}); t.textContent = n.findings.join("\n"); rect.appendChild(t); }
    node.appendChild(rect);
    if (n.module) {
      node.appendChild(svgEl("rect", { x: 6, y: 4, width: n.module.length * 6 + 8, height: 13, rx: 3, fill: "rgba(0,0,0,0.34)" }));
      const mt = svgEl("text", { x: 10, y: 13.5, "font-size": "9", fill: modColor[n.module], "font-weight": "700" });
      mt.textContent = n.module; node.appendChild(mt);
    }
    const name = n.local || n.name;
    const lbl = svgEl("text", { x: nodeW / 2, y: n.module ? nodeH / 2 + 9 : nodeH / 2 + 4, "text-anchor": "middle", fill: "#fff", "font-size": "12", "font-weight": "500" });
    lbl.textContent = name.length > 20 ? name.slice(0, 19) + "…" : name;
    node.appendChild(lbl);
    const sub = n.kind === "input" ? (n.type || "") : (n.hitPolicy || (n.decisionKind === "expression" ? "expr" : ""));
    if (sub) { const st = svgEl("text", { x: nodeW / 2, y: nodeH - 6, "text-anchor": "middle", fill: "#c5d0dc", "font-size": "9" }); st.textContent = sub; node.appendChild(st); }
    node.addEventListener("click", (ev) => { ev.stopPropagation(); selectNode(n); });
    svg.appendChild(node);
  });
  svg.addEventListener("click", () => { inspect.hidden = true; });

  const legend = document.createElement("div");
  legend.className = "graph-legend";
  let lg = `<span><i class="lg-pill" style="border-radius:9px;background:#243240"></i>input</span>`
    + `<span><i class="lg-pill" style="background:#1f6feb"></i>decision</span>`
    + `<span><i class="lg-pill" style="background:#e06b6b"></i>error</span>`
    + `<span><i class="lg-pill" style="background:#d9a441"></i>warning</span>`
    + `<span><i class="lg-pill lg-fired"></i>fired (last run)</span>`;
  if (edges.some((e) => e.crossModule)) lg += `<span><i class="lg-line"></i>cross-module</span>`;
  modules.forEach((m) => { lg += `<span><i class="lg-pill" style="background:${modColor[m]}"></i>${escapeHtml(m)}</span>`; });
  legend.innerHTML = lg;

  let vb = { x: 0, y: 0, w: W, h: H };
  const apply = () => svg.setAttribute("viewBox", `${vb.x} ${vb.y} ${vb.w} ${vb.h}`);
  const ptr = (ev) => { const r = svg.getBoundingClientRect(); return { x: vb.x + (ev.clientX - r.left) / r.width * vb.w, y: vb.y + (ev.clientY - r.top) / r.height * vb.h }; };
  const zoomAt = (f, ux, uy) => {
    const nw = Math.min(W * 6, Math.max(W * 0.15, vb.w * f));
    f = nw / vb.w;
    vb = { x: ux - (ux - vb.x) * f, y: uy - (uy - vb.y) * f, w: nw, h: vb.h * f };
    apply();
  };
  mkBtn("+", "zoom in", () => zoomAt(0.8, vb.x + vb.w / 2, vb.y + vb.h / 2));
  mkBtn("−", "zoom out", () => zoomAt(1.25, vb.x + vb.w / 2, vb.y + vb.h / 2));
  mkBtn("⤢", "fit", () => { vb = { x: 0, y: 0, w: W, h: H }; apply(); });
  svg.addEventListener("wheel", (ev) => { ev.preventDefault(); const p = ptr(ev); zoomAt(ev.deltaY < 0 ? 0.85 : 1.18, p.x, p.y); }, { passive: false });
  let drag = null;
  svg.addEventListener("mousedown", (ev) => { drag = { x: ev.clientX, y: ev.clientY }; svg.classList.add("grabbing"); });
  svg.addEventListener("mousemove", (ev) => { if (!drag) return; const r = svg.getBoundingClientRect(); vb.x -= (ev.clientX - drag.x) / r.width * vb.w; vb.y -= (ev.clientY - drag.y) / r.height * vb.h; drag = { x: ev.clientX, y: ev.clientY }; apply(); });
  const endDrag = () => { drag = null; svg.classList.remove("grabbing"); };
  svg.addEventListener("mouseup", endDrag);
  svg.addEventListener("mouseleave", endDrag);

  wrap.appendChild(controls);
  wrap.appendChild(svg);
  wrap.appendChild(inspect);
  container.appendChild(wrap);
  container.appendChild(legend);
  if (sim.lastTrace) highlightGraph(sim.lastTrace); // re-light after a source-driven re-render
}

// highlightGraph lights the decisions that actually fired on the last run and annotates each with its
// output value; un-exercised decision nodes are dimmed. It mutates the existing SVG in place so the
// user's zoom/pan survive an input change.
function highlightGraph(full) {
  const g = $("graph"); if (!g) return;
  const svg = g.querySelector("svg.drg"); if (!svg) return;
  svg.querySelectorAll(".gnode").forEach((n) => {
    n.classList.remove("fired", "dimmed");
    const c = n.querySelector(".val-chip"); if (c) c.remove();
  });
  if (!full || !full.path) return;
  const fired = {};
  full.path.forEach((t) => { if (t.matched) fired[t.decision] = t; });
  svg.querySelectorAll(".gnode").forEach((node) => {
    const name = node.getAttribute("data-name");
    const t = fired[name];
    if (t) {
      node.classList.add("fired");
      const chip = svgEl("g", { class: "val-chip" });
      const text = fmtVal(t.output);
      const short = text.length > 12 ? text.slice(0, 11) + "…" : text;
      const w = short.length * 6.4 + 12;
      chip.appendChild(svgEl("rect", { x: 170 - w + 2, y: -11, width: w, height: 18, rx: 9, fill: "#15281f", stroke: "#4caf7d", "stroke-width": "1" }));
      const txt = svgEl("text", { x: 170 - w / 2 + 2, y: 1.5, "text-anchor": "middle", fill: "#7fe3b0", "font-size": "10", "font-weight": "600" });
      txt.textContent = short;
      const title = svgEl("title", {}); title.textContent = `${name} = ${text}`; chip.appendChild(title);
      chip.appendChild(txt);
      node.appendChild(chip);
    } else if (node.getAttribute("data-kind") === "decision") {
      node.classList.add("dimmed");
    }
  });
}

// ====================================================================================
// traceability / source coverage (the "Traceability" button; distinct from the run trace)
// ====================================================================================
async function showTrace() {
  const src = $("source").value;
  const t = $("trace");
  if (!t) return;
  t.innerHTML = "";
  if (!src.trim()) return;
  const spec = $("ingest-source") ? $("ingest-source").value : "";
  const { ok, status, data } = await postJSON("/v1/trace", { rules: src, spec });
  if (!ok) {
    const e = data && data.error;
    t.textContent = e && e.message ? `compile error: ${e.message}` : `traceability failed (${status})`;
    return;
  }
  renderTrace(t, data);
}
function renderTrace(container, rep) {
  const cov = rep.coverage || {};
  const head = document.createElement("div");
  head.className = "headline";
  let txt = `${cov.decisionsSourced || 0}/${cov.decisions || 0} decisions cite a source`;
  if (cov.spansTotal) txt += ` · ${cov.spansCovered || 0}/${cov.spansTotal} source spans referenced (heuristic)`;
  head.textContent = txt;
  container.appendChild(head);
  if ((rep.untraced || []).length) {
    const u = document.createElement("div");
    u.className = "finding warning";
    u.innerHTML = `<span class="k">untraced</span>${escapeHtml(rep.untraced.join(", "))} — no @source`;
    container.appendChild(u);
  }
  // serve --ui returns {spans:[{span,covered,by}]}; the WASM playground returns {decisions:[{decision,source}]}.
  if (rep.spans) {
    (rep.spans || []).forEach((sp) => {
      const d = document.createElement("div");
      d.className = "span " + (sp.covered ? "covered" : "uncovered");
      const by = sp.by && sp.by.length ? " → " + escapeHtml(sp.by.join(", ")) : "";
      d.innerHTML = `<span class="mark">${sp.covered ? "✓" : "—"}</span><span class="txt">${escapeHtml(sp.span)}</span><span class="by">${by}</span>`;
      container.appendChild(d);
    });
  } else {
    (rep.decisions || []).filter((d) => d.source).forEach((d) => {
      const row = document.createElement("div");
      row.className = "span covered";
      row.innerHTML = `<span class="mark">✓</span><span class="txt">${escapeHtml(d.decision)}</span><span class="by">${escapeHtml(d.source)}</span>`;
      container.appendChild(row);
    });
  }
}

// ====================================================================================
// reactive simulator: always-visible typed inputs ⇄ JSON ⇄ result, all auto-updating
// ====================================================================================
// sim is the single source of truth. `input` holds the canonical values keyed by name:
//   number/string/date/duration -> string (exact text, never JS-Number-coerced)
//   boolean                      -> boolean
// `decInfo` carries per-decision unit/kind/hitPolicy for the result card.
const sim = { goal: "", meta: [], input: {}, decInfo: {}, syncing: false, lastTrace: null, runGen: 0 };

function parseEnum(domain) {
  const m = (domain || "").match(/in\s*\{(.+)\}/);
  if (!m) return null;
  return m[1].split(",").map((s) => s.trim().replace(/^"|"$/g, ""));
}
function parseRange(domain) {
  const m = (domain || "").match(/(in\s*)?([\[(])\s*(-?[\d.]+|-inf)\s*\.\.\s*(-?[\d.]+|\+inf)\s*([\])])/);
  if (!m) return null;
  return {
    min: m[3] === "-inf" ? null : m[3], max: m[4] === "+inf" ? null : m[4],
    loOpen: m[2] === "(", hiOpen: m[5] === ")",
  };
}
// fmtVal renders an engine value for display (objects/lists compacted, strings unquoted-ish).
function fmtVal(v) {
  if (v === null || v === undefined) return "null";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}
const NUMRE = /^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$/;

function defaultValue(inp) {
  if (inp.type === "boolean") return true;
  if (inp.type === "date") return "2020-01-01";
  if (inp.type === "duration") return "P365D";
  if (inp.type === "number") {
    const r = parseRange(inp.domain);
    return r && r.min != null ? r.min : (r && r.max != null ? r.max : "1");
  }
  const e = parseEnum(inp.domain);
  return e ? e[0] : "";
}

function populateGoalSelect(decisions) {
  const sel = $("goal");
  sim.decInfo = {};
  decisions.forEach((d) => { sim.decInfo[d.name] = d; });
  if (!sel) { if (decisions.length) sim.goal = decisions[decisions.length - 1].name; return; }
  const keep = decisions.some((d) => d.name === sim.goal) ? sim.goal : (decisions.length ? decisions[decisions.length - 1].name : "");
  sim.goal = keep;
  sel.innerHTML = "";
  decisions.forEach((d) => {
    const o = document.createElement("option");
    o.value = d.name;
    o.textContent = d.name + (d.hitPolicy ? `  [${d.hitPolicy}]` : "");
    if (d.name === keep) o.selected = true;
    sel.appendChild(o);
  });
}

// rebuildSim recomputes the whole simulator from the current source: goal list, the goal's required
// inputs, and the widgets — PRESERVING values whose (name,type) is unchanged so editing the rules
// doesn't wipe what the user typed. It then runs the goal.
async function rebuildSim() {
  const src = $("source").value;
  if (!src.trim()) { clearSim(); return; }
  const m = await postText("/v1/model", src);
  if (!m.ok || !m.data.decisions || !m.data.decisions.length) { clearSim(); return; }
  populateGoalSelect(m.data.decisions);
  await rebuildInputsForGoal();
}

async function rebuildInputsForGoal() {
  const src = $("source").value;
  if (!src.trim() || !sim.goal) { clearSim(); return; }
  const req = await postJSON("/v1/required", { rules: src, decision: sim.goal });
  const newMeta = (req.ok && req.data.inputs) ? req.data.inputs : [];
  const oldByName = {};
  sim.meta.forEach((x) => { oldByName[x.name] = x; });
  const merged = {};
  newMeta.forEach((inp) => {
    const old = oldByName[inp.name];
    if (old && old.type === inp.type && sim.input[inp.name] !== undefined) merged[inp.name] = sim.input[inp.name];
    else merged[inp.name] = defaultValue(inp);
  });
  sim.meta = newMeta;
  sim.input = merged;
  renderWidgets();
  syncJSONFromInput();
  await runGoal();
}

function clearSim() {
  sim.meta = []; sim.input = {}; sim.goal = ""; sim.lastTrace = null;
  const f = $("sim-form"); if (f) f.innerHTML = "";
  const r = $("result"); if (r) r.innerHTML = "";
  const sel = $("goal"); if (sel) sel.innerHTML = "";
  const inp = $("input"); if (inp) inp.value = "";
}

function renderWidgets() {
  const form = $("sim-form");
  if (!form) return;
  form.innerHTML = "";
  if (!sim.meta.length) { form.innerHTML = `<div class="sim-empty">This decision needs no inputs.</div>`; return; }
  sim.meta.forEach((inp) => form.appendChild(widget(inp)));
}

function widget(inp) {
  const wrap = document.createElement("label");
  wrap.className = "field";

  const head = document.createElement("span");
  head.className = "f-head";
  const cap = document.createElement("span");
  cap.className = "f-name";
  cap.textContent = inp.question || inp.title || inp.name;
  head.appendChild(cap);
  const meta = [inp.type];
  if (inp.unit) meta.push(inp.unit);
  if (inp.domain) meta.push(inp.domain);
  const hint = document.createElement("span");
  hint.className = "f-hint";
  hint.textContent = meta.join(" · ");
  head.appendChild(hint);
  if (inp.doc) wrap.title = inp.doc;
  wrap.appendChild(head);

  const row = document.createElement("span");
  row.className = "f-row";
  let el;
  const cur = sim.input[inp.name];
  const enumVals = parseEnum(inp.domain);
  if (inp.type === "boolean") {
    el = document.createElement("input"); el.type = "checkbox"; el.checked = !!cur;
  } else if (enumVals) {
    el = document.createElement("select");
    enumVals.forEach((v) => { const o = document.createElement("option"); o.value = v; o.textContent = v; if (v === cur) o.selected = true; el.appendChild(o); });
  } else if (inp.type === "number") {
    el = document.createElement("input"); el.type = "number"; el.step = "any";
    const rng = parseRange(inp.domain);
    if (rng) { if (rng.min != null) el.min = rng.min; if (rng.max != null) el.max = rng.max; }
    if (cur != null) el.value = cur;
  } else if (inp.type === "date") {
    el = document.createElement("input"); el.type = "date"; if (cur != null) el.value = cur;
  } else {
    el = document.createElement("input"); el.type = "text";
    if (inp.type === "duration") el.placeholder = "P30D";
    if (cur != null) el.value = cur;
  }
  el.dataset.name = inp.name;
  el.dataset.itype = inp.type;
  el.addEventListener("input", () => onWidgetChange(inp, el));
  el.addEventListener("change", () => onWidgetChange(inp, el));
  row.appendChild(el);

  const warn = document.createElement("span");
  warn.className = "f-warn";
  warn.dataset.for = inp.name;
  row.appendChild(warn);
  wrap.appendChild(row);
  validateField(inp, el.type === "checkbox" ? el.checked : el.value, warn);
  return wrap;
}

function onWidgetChange(inp, el) {
  const val = inp.type === "boolean" ? el.checked : el.value;
  sim.input[inp.name] = val;
  const warn = $("sim-form").querySelector(`.f-warn[data-for="${CSS.escape(inp.name)}"]`);
  if (warn) validateField(inp, val, warn);
  syncJSONFromInput();
  debouncedRun();
}

// validateField shows a NON-BLOCKING out-of-domain hint (the engine intentionally does not enforce
// domains at runtime — this is guidance only).
function validateField(inp, val, warn) {
  warn.textContent = "";
  warn.classList.remove("show");
  if (inp.type === "boolean") return;
  // Empty required input: flag it (the engine errors on a missing variable). runGoal suppresses the run
  // while any field is empty/invalid, so the user sees this field-level cue instead of a global error.
  if (val === "" || val == null) { warn.textContent = "⚠ required"; warn.classList.add("show"); return; }
  let msg = "";
  const enumVals = parseEnum(inp.domain);
  if (enumVals && !enumVals.includes(String(val))) msg = "not in domain";
  else if (inp.type === "number") {
    if (!NUMRE.test(String(val).trim())) msg = "not a number";
    else {
      const r = parseRange(inp.domain);
      if (r) {
        const x = Number(val);
        if (r.min != null && (r.loOpen ? x <= Number(r.min) : x < Number(r.min))) msg = `< ${r.min}`;
        else if (r.max != null && (r.hiOpen ? x >= Number(r.max) : x > Number(r.max))) msg = `> ${r.max}`;
      }
    }
  } else if (inp.type === "date" && !/^\d{4}-\d{2}-\d{2}$/.test(String(val))) msg = "expected YYYY-MM-DD";
  else if (inp.type === "duration" && !/^P/.test(String(val))) msg = "expected P…D";
  if (msg) { warn.textContent = "⚠ " + msg; warn.classList.add("show"); }
}

// buildInputJSON serializes sim.input to JSON, emitting number values as RAW number tokens (never via
// JS Number) so the engine's exact decimals survive. pretty=true for the editable textarea.
function buildInputJSON(pretty) {
  const entries = [];
  sim.meta.forEach((inp) => {
    const v = sim.input[inp.name];
    if (v === undefined || v === "") return;
    let token;
    if (inp.type === "boolean") token = v ? "true" : "false";
    else if (inp.type === "number") { const s = String(v).trim(); if (!NUMRE.test(s)) return; token = s; }
    else token = JSON.stringify(String(v));
    entries.push([inp.name, token]);
  });
  if (!entries.length) return "{}";
  if (!pretty) return "{" + entries.map(([k, t]) => `${JSON.stringify(k)}:${t}`).join(",") + "}";
  return "{\n" + entries.map(([k, t]) => `  ${JSON.stringify(k)}: ${t}`).join(",\n") + "\n}";
}

function syncJSONFromInput() {
  const box = $("input");
  if (!box) return;
  sim.syncing = true;
  box.value = buildInputJSON(true);
  const err = $("json-err"); if (err) { err.textContent = ""; err.classList.remove("show"); }
  sim.syncing = false;
}

// onJsonChange: the user edited the raw JSON. Validate; on success repopulate the widgets (display
// only) and run; on failure show an inline error WITHOUT destroying their text or the widgets.
function onJsonChange() {
  if (sim.syncing) return;
  const box = $("input");
  const err = $("json-err");
  const raw = box.value.trim();
  let obj;
  if (raw === "") obj = {};
  else {
    try { obj = JSON.parse(raw); }
    catch (e) { if (err) { err.textContent = "invalid JSON: " + e.message; err.classList.add("show"); } return; }
    if (obj === null || typeof obj !== "object" || Array.isArray(obj)) {
      if (err) { err.textContent = "input must be a JSON object"; err.classList.add("show"); }
      return;
    }
  }
  if (err) { err.textContent = ""; err.classList.remove("show"); }
  // Update canonical values for widget display. For numbers we keep the EXACT source token from the raw
  // textarea (not String(JSON.parse(...)), which would round through a JS f64 and then leak that rounded
  // value back into the submitted JSON the next time a widget is touched — silently breaking the
  // decimal-exact guarantee). The textarea itself is still submitted verbatim by runGoal.
  sim.meta.forEach((inp) => {
    if (!(inp.name in obj)) { delete sim.input[inp.name]; return; }
    const v = obj[inp.name];
    if (inp.type === "boolean") { sim.input[inp.name] = !!v; return; }
    if (inp.type === "number") {
      const tok = rawNumberToken(raw, inp.name);
      sim.input[inp.name] = tok != null ? tok : String(v);
      return;
    }
    sim.input[inp.name] = String(v);
  });
  renderWidgets();
  debouncedRun();
}

// rawNumberToken returns the exact source text of a top-level numeric value in the (flat, scalar) input
// JSON object, so a high-precision decimal pasted into the editor is preserved verbatim rather than
// rounded through JS Number. Returns null if the key is absent or its value is not a bare number.
function rawNumberToken(raw, key) {
  const k = JSON.stringify(String(key)); // exact quoted key; immune to prefix collisions ("a" vs "ab")
  const i = raw.indexOf(k);
  if (i < 0) return null;
  const m = /^\s*:\s*(-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(raw.slice(i + k.length));
  return m ? m[1] : null;
}

function setRecomputing(on) {
  const el = $("recompute");
  if (!el) return;
  el.textContent = on ? "computing…" : "live";
  el.className = "recompute" + (on ? " busy" : " idle");
}

// runGoal evaluates the goal with full justification (full:true => FullTrace). The input is taken from
// the textarea VERBATIM (already valid JSON) so exact decimal tokens reach the engine unaltered.
async function runGoal() {
  const src = $("source").value;
  const goal = sim.goal;
  const result = $("result");
  if (!src.trim() || !goal) { if (result) result.innerHTML = ""; return; }
  // Don't submit an incomplete object: if a required input is empty or not-yet-valid, the field-level
  // hints (validateField) already say so — skip the run instead of provoking a cryptic global
  // "unknown variable at execution time" engine error.
  if (missingRequired()) { setRecomputing(false); return; }
  const box = $("input");
  let rawJSON = box ? box.value.trim() : buildInputJSON(false);
  if (rawJSON === "") rawJSON = "{}";
  try { const o = JSON.parse(rawJSON); if (o === null || typeof o !== "object" || Array.isArray(o)) throw 0; }
  catch { return; } // invalid JSON: onJsonChange already surfaced it; don't run
  const myGen = ++sim.runGen; // stale-response guard: only the latest run may apply its result
  const body = `{"rules":${JSON.stringify(src)},"decision":${JSON.stringify(goal)},"input":${rawJSON},"full":true}`;
  setRecomputing(true);
  const { ok, status, data } = await api("/v1/run", { method: "POST", headers: { "Content-Type": "application/json" }, body });
  if (myGen !== sim.runGen) return; // a newer edit already fired a later run — drop this stale response
  setRecomputing(false);
  if (!ok) {
    sim.lastTrace = null;
    window.lastRun = null;
    if (result) {
      const e = data && data.error;
      result.innerHTML = `<div class="rc rc-err">${escapeHtml(e && e.message ? `error: ${e.message}` : `run failed (${status})`)}</div>`;
    }
    markActiveInputs(null);
    highlightGraph(null);
    return;
  }
  window.lastRun = { decision: goal, input: safeParse(rawJSON), output: data.output, trace: data.trace };
  sim.lastTrace = data.trace || null;
  renderResult(data, goal);
  markActiveInputs(data.trace);
  highlightGraph(data.trace);
}
const debouncedRun = debounce(runGoal, 160);

function safeParse(s) { try { return JSON.parse(s); } catch { return {}; } }

// missingRequired reports whether any displayed (goal-required) input lacks a usable value — empty, or
// a number still mid-typing/invalid (NUMRE). buildInputJSON drops such entries, which would make the
// engine reject the run with a global "unknown variable" error; runGoal skips the run instead.
function missingRequired() {
  return sim.meta.some((inp) => {
    if (inp.type === "boolean") return false; // a checkbox always has a value
    const v = sim.input[inp.name];
    if (v === undefined || v === "" || v == null) return true;
    if (inp.type === "number") return !NUMRE.test(String(v).trim());
    return false;
  });
}

// markActiveInputs dims the input widgets that PROVABLY did not contribute to the current outcome, so
// the correct-but-surprising "I changed an input and nothing happened" (a hit:first rule firing on an
// earlier column) is visible rather than looking broken. It only dims when attribution is certain:
//   • hit:first table whose FIRST rule won — later rules are never evaluated and there are no earlier
//     ones, so ONLY that rule's cited cells could matter; everything else is provably irrelevant.
// In every other case (a later rule won — earlier rules' columns may have gated the result; UNIQUE/ANY/
// COLLECT — all rules are evaluated; a pure expression) we cannot single out inputs from the trace, so
// we conservatively treat all of a decision's dependencies as active and dim nothing misleadingly.
function markActiveInputs(trace) {
  const form = $("sim-form");
  if (!form) return;
  const path = (trace && trace.path) || [];
  const traceByDec = {};
  path.forEach((t) => { traceByDec[t.decision] = t; });

  const active = new Set();
  const seen = new Set();
  const stack = [sim.goal];
  let canAttribute = false; // becomes true only when some decision gives a provable cell attribution
  while (stack.length) {
    const name = stack.pop();
    if (seen.has(name)) continue;
    seen.add(name);
    const t = traceByDec[name];
    const info = sim.decInfo[name];
    if (t && t.kind === "table" && t.hitPolicy === "first" && t.ruleIndex === 1 && t.cells && t.cells.length) {
      canAttribute = true;
      t.cells.forEach((c) => { active.add(c.input); stack.push(c.input); }); // only the deciding cells
    } else if (info && info.deps) {
      info.deps.forEach((d) => { active.add(d); stack.push(d); }); // can't attribute: every dependency may matter
    }
  }

  form.querySelectorAll(".field").forEach((field) => {
    const el = field.querySelector("[data-name]");
    const name = el && el.dataset.name;
    const inactive = canAttribute && !!name && !active.has(name);
    field.classList.toggle("inactive", inactive);
    let note = field.querySelector(".f-inactive");
    if (inactive && !note) {
      note = document.createElement("span");
      note.className = "f-inactive";
      note.textContent = "not used for this outcome";
      const head = field.querySelector(".f-head");
      if (head) head.appendChild(note);
    } else if (!inactive && note) {
      note.remove();
    }
  });
}

// renderResult draws the rich result card: goal → value (+unit), matched/fallback badge, and a
// readable per-decision justification built from the FullTrace (winning rule #, line, justifying
// cells, contributors, @source). A collapsed <details> keeps the raw JSON for power users.
function renderResult(data, goal) {
  const result = $("result");
  if (!result) return;
  result.innerHTML = "";
  const full = data.trace;
  const res = full && full.result ? full.result : null;
  const dec = sim.decInfo[goal] || {};
  const unit = dec.unit ? ` <span class="rc-unit">${escapeHtml(dec.unit)}</span>` : "";

  const card = document.createElement("div");
  card.className = "rc";
  let badge = "";
  if (res) {
    if (res.fallback) badge = `<span class="rc-badge fallback">default</span>`;
    else if (res.matched) badge = `<span class="rc-badge ok">matched</span>`;
    else badge = `<span class="rc-badge none">no match</span>`;
  }
  card.innerHTML = `<div class="rc-head"><span class="rc-goal">${escapeHtml(goal)}</span><span class="rc-arrow">→</span>`
    + `<span class="rc-val">${escapeHtml(fmtVal(data.output))}</span>${unit}${badge}</div>`;
  result.appendChild(card);

  if (full && full.path && full.path.length) {
    const tw = document.createElement("div");
    tw.className = "rc-trace";
    // dependency-first; show goal first for readability by reversing.
    full.path.slice().reverse().forEach((t) => tw.appendChild(traceRow(t, t.decision === goal)));
    result.appendChild(tw);
  }

  const raw = document.createElement("details");
  raw.className = "rc-raw";
  raw.innerHTML = `<summary>raw output & trace</summary><pre>${escapeHtml(JSON.stringify(data, null, 2))}</pre>`;
  result.appendChild(raw);
}

function traceRow(t, isGoal) {
  const row = document.createElement("div");
  row.className = "tr" + (isGoal ? " goal" : "");
  const dunit = (sim.decInfo[t.decision] && sim.decInfo[t.decision].unit) ? " " + sim.decInfo[t.decision].unit : "";
  let how = "";
  if (t.kind === "literal-expr") how = t.exprSrc ? `= ${escapeHtml(t.exprSrc)}` : "expression";
  else if (t.contributors && t.contributors.length) how = `${t.hitPolicy || "collect"} · ${t.contributors.length} rule(s)`;
  else if (t.matched && t.ruleIndex) how = `rule #${t.ruleIndex}${t.ruleLine ? " (line " + t.ruleLine + ")" : ""}`;
  else if (t.fallback) how = "default";
  else if (!t.matched) how = "no match";
  let cells = "";
  if (t.cells && t.cells.length) {
    cells = `<div class="tr-cells">` + t.cells.map((c) =>
      `<span class="cell"><b>${escapeHtml(c.input)}</b> ${escapeHtml(c.value)} <em>⊨ ${escapeHtml(c.src)}</em></span>`).join("") + `</div>`;
  }
  const src = t.source ? `<span class="tr-src" title="@source">${escapeHtml(t.source)}</span>` : "";
  row.innerHTML = `<div class="tr-head"><span class="tr-dec">${escapeHtml(t.title || t.decision)}</span>`
    + `<span class="tr-arrow">→</span><span class="tr-val">${escapeHtml(fmtVal(t.output))}${escapeHtml(dunit)}</span>`
    + `<span class="tr-how">${how}</span>${src}</div>${cells}`;
  return row;
}

// ====================================================================================
// live loop & wiring
// ====================================================================================
// source edits rebuild verify + graph + the simulator (preserving input values); input/JSON edits only
// re-run. refreshAll runs the whole pipeline immediately (used by project.js on module switch).
async function refreshAll() {
  await verify();
  await showGraph();
  await rebuildSim();
}
const onSourceChange = debounce(refreshAll, 300);

async function onGoalChange() {
  const sel = $("goal");
  if (sel) sim.goal = sel.value;
  await rebuildInputsForGoal();
}

// initSim wires the reactive simulator. Hosts call it after the engine is ready and the initial source
// is in place. It is idempotent-friendly: only the elements that exist are wired.
function initSim() {
  const src = $("source");
  if (src) src.addEventListener("input", onSourceChange);
  const box = $("input");
  if (box) box.addEventListener("input", onJsonChange);
  const goal = $("goal");
  if (goal) goal.addEventListener("change", onGoalChange);
  setRecomputing(false);
  if (src && src.value.trim()) refreshAll();
}

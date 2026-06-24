"use strict";
// feelc authoring UI — zero dependencies. The LLM only AUTHORS; every result shown comes from the
// deterministic engine (/v1/verify, /v1/run). Config is bring-your-own and lives in localStorage.

const $ = (id) => document.getElementById(id);
const LS_KEY = "feelc.llm";
const messages = []; // {role, content}

// ---- LLM settings (localStorage) ----
function loadCfg() {
  try { return JSON.parse(localStorage.getItem(LS_KEY)) || {}; } catch { return {}; }
}
function saveCfg(cfg) { localStorage.setItem(LS_KEY, JSON.stringify(cfg)); }
function refreshStatus() {
  const c = loadCfg();
  const el = $("llm-status");
  const ready = !!c.apiKey;
  el.textContent = ready ? `LLM: ${c.provider || "anthropic"} · ${c.model || "default"}` : "LLM: not connected — click to connect";
  el.classList.toggle("ok", ready);
  document.body.classList.toggle("llm-ready", ready);
  // Empty-state invitation: only while the conversation hasn't started.
  const thread = $("thread");
  const card = thread.querySelector(".connect-card");
  if (!ready && !messages.length) {
    if (!card) thread.appendChild(connectCard());
  } else if (card) {
    card.remove();
  }
}

// connectCard is the inviting empty-state shown until an LLM is connected: bring-your-own-model + a pointer
// to the portable skill (the same red→green loop outside the browser).
function connectCard() {
  const card = document.createElement("div");
  card.className = "connect-card";
  card.innerHTML =
    `<div class="cc-title">Connect your AI to start authoring</div>` +
    `<div class="cc-sub">Bring your own model — Anthropic (Claude), OpenAI, or any compatible endpoint. ` +
    `Your key stays in your browser; the engine never calls an LLM at runtime.</div>` +
    `<button type="button" class="primary cc-btn">Connect your AI</button>` +
    `<div class="cc-skill">Prefer your editor or a coding agent? The same draft→verify→repair loop is a portable ` +
    `<a href="https://github.com/maxgfr/feelc/tree/main/skill" target="_blank" rel="noopener">feelc skill</a>.</div>`;
  card.querySelector(".cc-btn").addEventListener("click", openSettings);
  return card;
}

// ---- HTTP helper ----
async function api(path, opts) {
  const res = await fetch(path, opts);
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { raw: text }; }
  return { ok: res.ok, status: res.status, data };
}

// ---- chat ----
function escapeHtml(s) {
  return s.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}
function renderMessage(content) {
  // Render fenced ``` blocks as <pre>, the rest as escaped text.
  const parts = content.split(/```/);
  return parts.map((p, i) => {
    if (i % 2 === 1) {
      const body = p.replace(/^[a-zA-Z]*\n/, ""); // drop the language tag line
      return `<pre><code>${escapeHtml(body)}</code></pre>`;
    }
    return escapeHtml(p);
  }).join("");
}
function pushMsg(role, content) {
  messages.push({ role, content });
  const card = $("thread").querySelector(".connect-card");
  if (card) card.remove();
  const div = document.createElement("div");
  div.className = `msg ${role}`;
  div.innerHTML = role === "assistant" ? renderMessage(content) : escapeHtml(content);
  $("thread").appendChild(div);
  $("thread").scrollTop = $("thread").scrollHeight;
}
function pushError(text) {
  const div = document.createElement("div");
  div.className = "msg error";
  div.textContent = text;
  $("thread").appendChild(div);
  $("thread").scrollTop = $("thread").scrollHeight;
}

async function sendChat(e) {
  e.preventDefault();
  const input = $("chat-input");
  const prompt = input.value.trim();
  if (!prompt) return;
  const cfg = loadCfg();
  input.value = "";
  pushMsg("user", prompt);
  const btn = $("send-btn");
  btn.disabled = true; btn.textContent = "…";
  try {
    // In project mode, project.js redirects to /v1/project/chat (lexical retrieval) for the selected
    // module; otherwise this is the plain single-model /v1/chat.
    let spec = (typeof projectChatRequest === "function") ? projectChatRequest(messages, cfg) : null;
    if (!spec) spec = { path: "/v1/chat", body: JSON.stringify({ messages, llm: cfg }) };
    const { ok, status, data } = await api(spec.path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: spec.body,
    });
    if (status === 501) {
      pushError("No LLM configured. Open ⚙ LLM settings to add a provider, model and API key.");
      $("settings").showModal();
      return;
    }
    if (!ok) { pushError(`Chat failed (${status}): ${data?.error || "unknown error"}`); return; }
    pushMsg("assistant", data.message || "(empty reply)");
    if (data.rules) {
      $("source").value = data.rules;
      detectDecision();
      if ($("auto-verify").checked) verify();
    }
  } catch (err) {
    pushError("Network error: " + err.message);
  } finally {
    btn.disabled = false; btn.textContent = "Send";
  }
}

// ---- ingest (spec -> draft -> verify -> bounded repair) ----
// The LLM drafts @source-annotated rules from an arbitrary spec; the engine verifies/repairs them.
// Everything shown comes from the deterministic engine — the LLM only authors the .rules text.
async function ingest() {
  const source = $("ingest-source").value.trim();
  if (!source) return;
  const btn = $("ingest-btn");
  btn.disabled = true; btn.textContent = "…";
  clearReport();
  banner("ok", "Drafting rules from the specification, then verifying & repairing…");
  try {
    const { ok, status, data } = await api("/v1/ingest", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ source, maxRounds: Number($("ingest-rounds").value) || 3, llm: loadCfg() }),
    });
    if (status === 501) {
      clearReport();
      banner("err", "No LLM configured. Open ⚙ LLM settings to add a provider, model and API key.");
      $("settings").showModal();
      return;
    }
    if (!ok) { clearReport(); banner("err", `ingest failed (${status}): ${data?.error || "unknown error"}`); return; }
    if (data.rules) { $("source").value = data.rules; detectDecision(); }
    clearReport();
    $("report").appendChild(renderIngestSteps(data.rounds, data.converged, data.blockers));
    ((data.verify && data.verify.findings) || []).forEach(renderFinding);
    await showTrace();
    await showGraph();
  } catch (err) {
    clearReport(); banner("err", "Network error: " + err.message);
  } finally {
    btn.disabled = false; btn.textContent = "Ingest";
  }
}

// renderIngestSteps visualizes the deterministic red→green loop: the LLM drafts, then each round the
// engine verifies and the blocker count drives the next repair — until it converges (or stops). The engine,
// not the LLM, decides when it's done.
function renderIngestSteps(rounds, converged, blockers) {
  const wrap = document.createElement("div");
  wrap.className = "steps";
  const add = (cls, label, sub) => {
    const s = document.createElement("div");
    s.className = "step " + cls;
    s.innerHTML = `<span class="s-label">${escapeHtml(label)}</span>` + (sub ? `<span class="s-sub">${escapeHtml(sub)}</span>` : "");
    wrap.appendChild(s);
  };
  add("done", "Draft", "LLM");
  (rounds || []).forEach((r) => {
    if (r.compileError) add("bad", "Round " + r.n, "compile error");
    else add(r.blockers > 0 ? "bad" : "ok", "Round " + r.n, r.blockers + (r.blockers === 1 ? " blocker" : " blockers"));
  });
  add(converged ? "ok" : "bad", converged ? "✓ Converged" : "Stopped",
    converged ? "proved complete & consistent" : (blockers || 0) + " blocker(s) left");
  return wrap;
}

// ---- traceability + coverage (LLM-free) ----
async function showTrace() {
  const src = $("source").value;
  const t = $("trace");
  t.innerHTML = "";
  if (!src.trim()) return;
  const { ok, status, data } = await api("/v1/trace", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ rules: src, spec: $("ingest-source").value }),
  });
  if (!ok) {
    const e = data?.error;
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
  (rep.spans || []).forEach((sp) => {
    const d = document.createElement("div");
    d.className = "span " + (sp.covered ? "covered" : "uncovered");
    const by = sp.by && sp.by.length ? " → " + escapeHtml(sp.by.join(", ")) : "";
    d.innerHTML = `<span class="mark">${sp.covered ? "✓" : "—"}</span><span class="txt">${escapeHtml(sp.span)}</span><span class="by">${by}</span>`;
    container.appendChild(d);
  });
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
  if (!src.trim()) return;
  clearReport();
  const { ok, status, data } = await api("/v1/verify", {
    method: "POST",
    headers: { "Content-Type": "text/plain" },
    body: src,
  });
  if (!ok) {
    const e = data?.error;
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

// ---- graph (DRG) ----
const SVGNS = "http://www.w3.org/2000/svg";
function svgEl(name, attrs) {
  const el = document.createElementNS(SVGNS, name);
  for (const k in attrs) el.setAttribute(k, attrs[k]);
  return el;
}
async function showGraph() {
  const src = $("source").value;
  if (!src.trim()) return;
  const g = $("graph");
  g.innerHTML = "";
  const { ok, status, data } = await api("/v1/graph", {
    method: "POST", headers: { "Content-Type": "text/plain" }, body: src,
  });
  if (!ok) {
    const e = data?.error;
    g.textContent = e && e.message ? `compile error${e.line ? " (line " + e.line + ")" : ""}: ${e.message}` : `graph failed (${status})`;
    return;
  }
  renderGraph(g, data.graph);
}
// Per-module accent palette (project mode): each module gets a stable colour for node borders + legend.
const MOD_PALETTE = ["#6ea8fe", "#7fd1ae", "#e0a458", "#c08cf0", "#e88aa8", "#5ec8c8", "#b5c46b"];

// renderGraph lays the DRG out as a left-to-right layered SVG (zero dependency, offline) and makes it
// interactive: wheel-zoom + drag-pan + fit, a legend, click-a-node-to-inspect, module-coloured borders and
// dashed cross-module edges. Rank 0 = inputs; a decision's rank is 1 + the max rank of its dependencies.
function renderGraph(container, graph) {
  const nodes = graph.nodes || [], edges = graph.edges || [];
  container.innerHTML = "";
  if (!nodes.length) { container.textContent = "nothing to graph yet"; return; }

  // Layered ranks (fixpoint over the edges).
  const rank = {};
  nodes.forEach((n) => { rank[n.id] = 0; });
  for (let pass = 0; pass <= nodes.length; pass++) {
    let changed = false;
    edges.forEach((e) => { const r = (rank[e.from] || 0) + 1; if (r > (rank[e.into] || 0)) { rank[e.into] = r; changed = true; } });
    if (!changed) break;
  }

  // Modules → stable colours.
  const modules = [...new Set(nodes.map((n) => n.module).filter(Boolean))];
  const modColor = {};
  modules.forEach((m, i) => { modColor[m] = MOD_PALETTE[i % MOD_PALETTE.length]; });

  // Columns by rank; within a column, group by module then name for tidy adjacency.
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

  // Zoom/pan controls.
  const controls = document.createElement("div");
  controls.className = "graph-controls";
  const mkBtn = (label, title, fn) => { const b = document.createElement("button"); b.type = "button"; b.textContent = label; b.title = title; b.onclick = fn; controls.appendChild(b); };

  // SVG with a marker per edge style.
  const svg = svgEl("svg", { class: "drg", width: "100%", viewBox: `0 0 ${W} ${H}` });
  const defs = svgEl("defs", {});
  const mkMarker = (id, color) => { const m = svgEl("marker", { id, viewBox: "0 0 10 10", refX: "9", refY: "5", markerWidth: "7", markerHeight: "7", orient: "auto-start-reverse" }); m.appendChild(svgEl("path", { d: "M0,0 L10,5 L0,10 z", fill: color })); return m; };
  defs.appendChild(mkMarker("arrow", "#6b7785"));
  defs.appendChild(mkMarker("arrowx", "#c08cf0"));
  svg.appendChild(defs);

  // Edges (dashed + purple when cross-module).
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

  // Inspect panel (populated on node click).
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

  // Nodes.
  const sevFill = { error: "#e06b6b", warning: "#d9a441", info: "#7fb3ff" };
  nodes.forEach((n) => {
    const p = pos[n.id];
    if (!p) return;
    const node = svgEl("g", { class: "gnode", transform: `translate(${p.x},${p.y})` });
    const fill = sevFill[n.severity] || (n.kind === "input" ? "#243240" : "#1f6feb");
    const stroke = n.module ? modColor[n.module] : "#2a3340";
    const rect = svgEl("rect", { x: 0, y: 0, width: nodeW, height: nodeH, rx: n.kind === "input" ? 22 : 8, fill, stroke, "stroke-width": n.module ? "2" : "1" });
    if (n.findings && n.findings.length) { const t = svgEl("title", {}); t.textContent = n.findings.join("\n"); rect.appendChild(t); }
    node.appendChild(rect);
    if (n.module) {
      // a dark chip keeps the module tag legible on any node fill (severity colours, light blues, …)
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

  // Legend.
  const legend = document.createElement("div");
  legend.className = "graph-legend";
  let lg = `<span><i class="lg-pill" style="border-radius:9px;background:#243240"></i>input</span>`
    + `<span><i class="lg-pill" style="background:#1f6feb"></i>decision</span>`
    + `<span><i class="lg-pill" style="background:#e06b6b"></i>error</span>`
    + `<span><i class="lg-pill" style="background:#d9a441"></i>warning</span>`;
  if (edges.some((e) => e.crossModule)) lg += `<span><i class="lg-line"></i>cross-module</span>`;
  modules.forEach((m) => { lg += `<span><i class="lg-pill" style="background:${modColor[m]}"></i>${escapeHtml(m)}</span>`; });
  legend.innerHTML = lg;

  // viewBox zoom/pan (no library): wheel zooms toward the cursor, drag pans, ⤢ fits.
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
}

// ---- simulator (question-flow) ----
// buildForm asks the engine which inputs the chosen decision actually needs, then renders a minimal
// typed form (only those inputs) with widgets derived from each input's domain.
async function buildForm() {
  const decision = $("decision").value.trim();
  const form = $("sim-form");
  form.innerHTML = "";
  if (!decision) { form.textContent = "Enter a decision name first."; return; }
  const { ok, status, data } = await api("/v1/required", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ rules: $("source").value, decision }),
  });
  if (!ok) {
    const e = data?.error;
    form.textContent = e && e.message ? `error: ${e.message}` : `cannot build form (${status}): ${data?.error || ""}`;
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
  $("input").value = JSON.stringify(input); // mirror into the JSON box for transparency
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
let lastRun = null; // {decision, input, output, trace} — for the "Explain" narration
async function runDecision(decision, input) {
  $("narration").textContent = "";
  const { ok, status, data } = await api("/v1/run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ rules: $("source").value, decision, input, explain: true }),
  });
  if (!ok) {
    const e = data?.error;
    $("output").textContent = (e && e.message) ? `error: ${e.message}` : `run failed (${status}): ${data?.error || ""}`;
    lastRun = null;
    return;
  }
  lastRun = { decision, input, output: data.output, trace: data.trace };
  let txt = "→ " + JSON.stringify(data.output);
  if (data.trace) txt += "\n\ntrace:\n" + JSON.stringify(data.trace, null, 2);
  $("output").textContent = txt;
}

// ---- AI explain (narrate a deterministic trace) ----
async function explainLast() {
  if (!lastRun) { $("narration").textContent = "Run a decision first, then Explain."; return; }
  $("narration").textContent = "…";
  const { ok, status, data } = await api("/v1/assist", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ task: "explain", payload: lastRun, llm: loadCfg() }),
  });
  if (status === 501) { $("narration").textContent = "Configure your LLM (⚙) to use AI explanations."; return; }
  $("narration").textContent = ok ? (data.message || "") : `explain failed (${status}): ${data?.error || ""}`;
}

// ---- AI test generation (draft claims -> check deterministically) ----
async function genTests() {
  if (!$("source").value.trim()) return;
  clearReport();
  banner("ok", "Asking the LLM to draft test cases…");
  const { ok, status, data } = await api("/v1/assist", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ task: "tests", payload: { rules: $("source").value }, llm: loadCfg() }),
  });
  clearReport();
  if (status === 501) { banner("err", "Configure your LLM (⚙) to generate tests."); return; }
  if (!ok) { banner("err", `tests failed (${status}): ${data?.error || ""}`); return; }
  let claims;
  try { claims = JSON.parse(firstJSONArray(data.message)); } catch (e) { banner("err", "LLM did not return a JSON array of claims."); return; }
  const res = await api("/v1/check", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ rules: $("source").value, claims }),
  });
  clearReport();
  if (!res.ok) { banner("err", `check failed (${res.status})`); return; }
  const findings = (res.data.report && res.data.report.findings) || [];
  banner(res.data.blockers > 0 ? "err" : "ok", `${claims.length} generated claim(s) checked — ${res.data.blockers} disagreement(s)`);
  findings.forEach(renderFinding);
}
// firstJSONArray extracts the first top-level [...] block from a reply (the LLM may wrap it in prose).
function firstJSONArray(s) {
  const start = s.indexOf("[");
  const end = s.lastIndexOf("]");
  return start >= 0 && end > start ? s.slice(start, end + 1) : s;
}

// ---- wiring ----
function openSettings() {
  const c = loadCfg();
  $("cfg-provider").value = c.provider || "anthropic";
  $("cfg-model").value = c.model || "";
  $("cfg-baseurl").value = c.baseURL || "";
  $("cfg-key").value = c.apiKey || "";
  $("settings").showModal();
}
function onSettingsClose() {
  // The form uses method="dialog"; save only when the Save button was the submitter.
  if ($("settings").returnValue === "save") {
    saveCfg({
      provider: $("cfg-provider").value,
      model: $("cfg-model").value.trim(),
      baseURL: $("cfg-baseurl").value.trim(),
      apiKey: $("cfg-key").value,
    });
    refreshStatus();
  }
}

window.addEventListener("DOMContentLoaded", () => {
  refreshStatus();
  $("chat-form").addEventListener("submit", sendChat);
  $("verify-btn").addEventListener("click", verify);
  $("graph-btn").addEventListener("click", showGraph);
  $("trace-btn").addEventListener("click", showTrace);
  $("ingest-btn").addEventListener("click", ingest);
  $("run-btn").addEventListener("click", run);
  $("form-btn").addEventListener("click", buildForm);
  $("explain-btn").addEventListener("click", explainLast);
  $("tests-btn").addEventListener("click", genTests);
  $("settings-btn").addEventListener("click", openSettings);
  $("llm-status").addEventListener("click", openSettings);
  $("settings").addEventListener("close", onSettingsClose);
  $("source").addEventListener("input", detectDecision);
  // Submit chat on Cmd/Ctrl+Enter.
  $("chat-input").addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") $("chat-form").requestSubmit();
  });
});

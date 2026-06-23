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
  if (c.apiKey) {
    el.textContent = `LLM: ${c.provider || "anthropic"} · ${c.model || "default"}`;
    el.classList.add("ok");
  } else {
    el.textContent = "LLM: not configured";
    el.classList.remove("ok");
  }
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
    const { ok, status, data } = await api("/v1/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ messages, llm: cfg }),
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
// renderGraph lays the DRG out as a left-to-right layered SVG (zero dependency, offline). Rank 0 =
// inputs; a decision's rank is 1 + the max rank of its dependencies (a fixpoint over the edges).
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
  const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, width: "100%", height: String(Math.min(H, 460)) });
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
  $("run-btn").addEventListener("click", run);
  $("form-btn").addEventListener("click", buildForm);
  $("explain-btn").addEventListener("click", explainLast);
  $("tests-btn").addEventListener("click", genTests);
  $("settings-btn").addEventListener("click", openSettings);
  $("settings").addEventListener("close", onSettingsClose);
  $("source").addEventListener("input", detectDecision);
  // Submit chat on Cmd/Ctrl+Enter.
  $("chat-input").addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") $("chat-form").requestSubmit();
  });
});

"use strict";
// Project-mode UI (additive, zero-dependency). Activates ONLY when GET /v1/project returns 200
// (i.e. `feelc serve --project`); in single-file mode it does nothing and the existing UI is untouched.
// Reuses shared.js/app.js globals: $, api, renderGraph, banner, clearReport, escapeHtml, verify, refreshAll.

let PROJECT = null;
let CURRENT_MODULE = null;

async function initProject() {
  const { ok, status, data } = await api("/v1/project");
  if (!ok || status === 404 || !data || !data.modules) return; // single-file mode
  PROJECT = data;
  document.body.classList.add("project-mode");
  buildRail();
  await loadHealth();
  if (PROJECT.modules.length) selectModule(PROJECT.modules[0].name);
}

function buildRail() {
  const main = document.querySelector("main");
  let rail = $("project-rail");
  if (!rail) {
    rail = document.createElement("section");
    rail.className = "pane project-rail";
    rail.id = "project-rail";
    main.insertBefore(rail, main.firstChild);
  }
  rail.innerHTML = "";

  const h = document.createElement("h2");
  h.textContent = PROJECT.name || "project";
  rail.appendChild(h);

  const status = document.createElement("div");
  status.className = "proj-status";
  status.id = "proj-status";
  rail.appendChild(status);

  const list = document.createElement("div");
  list.className = "module-list";
  list.id = "module-list";
  rail.appendChild(list);
  renderModuleList();

  const actions = document.createElement("div");
  actions.className = "rail-actions";
  if (PROJECT.editable) actions.appendChild(railButton("+ New module", createModulePrompt));
  actions.appendChild(railButton("Health", showDashboard));
  actions.appendChild(railButton("Graph", showProjectGraph));
  rail.appendChild(actions);
  if (!PROJECT.editable) {
    const ro = document.createElement("div");
    ro.className = "proj-readonly";
    ro.textContent = "read-only (start with --allow-edit to edit)";
    rail.appendChild(ro);
  }

  const dash = document.createElement("div");
  dash.className = "dashboard";
  dash.id = "dashboard";
  rail.appendChild(dash);
}

function railButton(label, fn) {
  const b = document.createElement("button");
  b.textContent = label;
  b.addEventListener("click", fn);
  return b;
}

function renderModuleList() {
  const list = $("module-list");
  list.innerHTML = "";
  (PROJECT.modules || []).forEach((m) => {
    const item = document.createElement("div");
    const isActive = m.name === CURRENT_MODULE;
    item.className = "module-item" + (isActive ? " active" : "");
    item.setAttribute("role", "button");
    item.tabIndex = 0;
    item.setAttribute("aria-label", `module ${m.name}${m.blockers > 0 ? `, ${m.blockers} blocker(s)` : ""}`);
    if (isActive) item.setAttribute("aria-current", "true");
    item.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); selectModule(m.name); }
    });

    const dot = document.createElement("span");
    dot.className = "dot " + (m.blockers > 0 ? "err" : "ok");
    item.appendChild(dot);

    const name = document.createElement("span");
    name.className = "mname";
    name.textContent = m.name;
    item.appendChild(name);

    if (m.blockers > 0) {
      const b = document.createElement("span");
      b.className = "badge";
      b.textContent = m.blockers;
      item.appendChild(b);
    }

    item.addEventListener("click", () => selectModule(m.name));

    if (PROJECT.editable) {
      const del = document.createElement("button");
      del.className = "del";
      del.textContent = "×";
      del.title = "Delete module";
      del.addEventListener("click", (e) => { e.stopPropagation(); deleteModule(m.name); });
      item.appendChild(del);
    }

    list.appendChild(item);
  });
}

async function refreshProject() {
  const { ok, data } = await api("/v1/project");
  if (ok && data && data.modules) { PROJECT = data; renderModuleList(); await loadHealth(); }
}

async function selectModule(name) {
  CURRENT_MODULE = name;
  renderModuleList();
  const res = await fetch(`/v1/modules/${encodeURIComponent(name)}/source`);
  $("source").value = res.ok ? await res.text() : "";
  ensureSaveButton();
  // refreshAll (shared.js) re-verifies, re-graphs and rebuilds the reactive simulator for this module.
  if (typeof refreshAll === "function") refreshAll();
}

function ensureSaveButton() {
  if (!PROJECT || !PROJECT.editable) return; // read-only mode: no Save control
  const actions = document.querySelector(".editor .actions");
  if (!actions || $("save-module-btn")) return;
  const btn = document.createElement("button");
  btn.id = "save-module-btn";
  btn.className = "primary";
  btn.textContent = "Save";
  btn.title = "Persist this module to disk (recompiles + verifies the whole project first)";
  btn.addEventListener("click", saveModule);
  actions.insertBefore(btn, actions.firstChild);
}

async function saveModule() {
  if (!CURRENT_MODULE) return;
  const btn = $("save-module-btn");
  btn.disabled = true; btn.textContent = "…";
  const { ok, status, data } = await api(`/v1/modules/${encodeURIComponent(CURRENT_MODULE)}/source`, {
    method: "PUT", headers: { "Content-Type": "text/plain" }, body: $("source").value,
  });
  btn.disabled = false; btn.textContent = "Save";
  clearReport();
  if (!ok) {
    const e = data && data.error;
    const msg = e && e.message ? `not saved — compile error${e.line ? " (line " + e.line + ")" : ""}: ${e.message}`
                               : `not saved (${status}): ${(e && (e.message || e)) || ""}`;
    banner("err", msg);
    return;
  }
  banner(data.status === "blocked" ? "err" : "ok", `saved · project ${data.status} · ${data.blockers} blocker(s)`);
  await refreshProject();
}

// modalDialog builds an accessible native <dialog> (focus-trapped, Esc-closable, styled by theme.css)
// and resolves to the clicked button's value. `fields` is optional markup placed above the actions.
function modalDialog({ body, confirmLabel = "OK", danger = false, focusSel }) {
  return new Promise((resolve) => {
    const dlg = document.createElement("dialog");
    dlg.innerHTML =
      `<form method="dialog" class="mini-form">${body}` +
      `<div class="dialog-actions">` +
      `<button value="cancel" class="ghost">Cancel</button>` +
      `<button value="ok" class="primary">${escapeHtml(confirmLabel)}</button>` +
      `</div></form>`;
    document.body.appendChild(dlg);
    dlg.addEventListener("close", () => { const v = dlg.returnValue; dlg.remove(); resolve(v); });
    dlg.showModal();
    const f = focusSel && dlg.querySelector(focusSel);
    if (f) f.focus();
    void danger;
  });
}

async function createModulePrompt() {
  const dlg = document.createElement("dialog");
  dlg.innerHTML =
    `<form method="dialog" class="mini-form">` +
    `<label class="mini-label">New module name <span class="hint">(letters, digits, underscore)</span>` +
    `<input class="mini-input" id="mini-mod-name" placeholder="pricing" autocomplete="off" /></label>` +
    `<div class="dialog-actions"><button value="cancel" class="ghost">Cancel</button>` +
    `<button value="ok" class="primary">Create</button></div></form>`;
  document.body.appendChild(dlg);
  const input = dlg.querySelector(".mini-input");
  const name = await new Promise((resolve) => {
    dlg.addEventListener("close", () => { const ok = dlg.returnValue === "ok"; const v = input.value.trim(); dlg.remove(); resolve(ok ? v : null); });
    dlg.showModal(); input.focus();
  });
  if (!name) return;
  const tmpl = `model "${name}" {\n  rounding: half_even\n}\n\ninput x : number\n\ndecision y : number = x\n`;
  const { ok, status, data } = await api("/v1/modules", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, source: tmpl }),
  });
  if (!ok) { toast(`create failed (${status}): ${errText(data)}`, "err"); return; }
  await refreshProject();
  selectModule(name);
  toast(`module "${name}" created`, "ok");
}

async function deleteModule(name) {
  const v = await modalDialog({
    body: `<p>Delete module <b>${escapeHtml(name)}</b>? This removes its <code>.rules</code> file.</p>`,
    confirmLabel: "Delete", danger: true,
  });
  if (v !== "ok") return;
  const { ok, status, data } = await api(`/v1/modules/${encodeURIComponent(name)}`, { method: "DELETE" });
  if (!ok) { toast(`delete failed (${status}): ${errText(data)}`, "err"); return; }
  if (CURRENT_MODULE === name) CURRENT_MODULE = null;
  await refreshProject();
  if (PROJECT.modules.length && !CURRENT_MODULE) selectModule(PROJECT.modules[0].name);
  toast(`module "${name}" deleted`, "ok");
}

function errText(data) {
  const e = data && data.error;
  return (e && (e.message || e)) || "unknown error";
}

async function loadHealth() {
  const { ok, data } = await api("/v1/project/health");
  const s = $("proj-status");
  if (!s) return;
  if (!ok) { s.textContent = ""; return; }
  s.className = "proj-status " + (data.status || "");
  s.textContent = `${data.status} · ${data.blockers} blocker(s)`;
}

async function showDashboard() {
  const dash = $("dashboard");
  dash.innerHTML = "";
  const { ok, data } = await api("/v1/project/health");
  if (!ok) { dash.textContent = "health unavailable"; return; }
  const rep = data.report || {};
  const table = document.createElement("table");
  table.className = "health";
  table.innerHTML = "<thead><tr><th>module</th><th>gap</th><th>conf</th><th>dead</th><th>blk</th></tr></thead>";
  const tb = document.createElement("tbody");
  (rep.modules || []).forEach((m) => {
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${escapeHtml(m.module)}</td><td>${m.gaps}</td><td>${m.conflicts}</td>` +
      `<td>${m.deadRules}</td><td class="${m.blockers > 0 ? "err" : "ok"}">${m.blockers}</td>`;
    tb.appendChild(tr);
  });
  table.appendChild(tb);
  dash.appendChild(table);
  (rep.crossModule || []).forEach((a) => {
    const d = document.createElement("div");
    d.className = "finding info";
    d.innerHTML = `<span class="k">${escapeHtml(a.kind)}</span>${escapeHtml(a.message)}`;
    dash.appendChild(d);
  });
}

async function showProjectGraph() {
  const g = $("graph");
  g.innerHTML = "";
  const { ok, data } = await api("/v1/project/graph");
  if (!ok) { g.textContent = "graph unavailable"; return; }
  renderGraph(g, data.graph);
}

// projectChatRequest redirects the chat to the project-aware endpoint (lexical retrieval for the selected
// module) when a module is selected; returns null otherwise so app.js falls back to /v1/chat.
function projectChatRequest(messages, cfg) {
  if (!PROJECT || !CURRENT_MODULE) return null;
  return {
    path: "/v1/project/chat",
    body: JSON.stringify({ messages, module: CURRENT_MODULE, llm: cfg }),
  };
}

window.addEventListener("DOMContentLoaded", initProject);

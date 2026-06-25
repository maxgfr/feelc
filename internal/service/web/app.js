"use strict";
// feelc authoring UI (host) — zero dependencies. The LLM only AUTHORS; every result shown comes from
// the deterministic engine. The renderers + reactive simulator live in shared.js (the single source of
// truth shared with the WASM playground); this file only adds the HTTP transport, chat, ingest, AI
// settings and the serve-only "Explain"/"AI tests" affordances, then hands off to initSim().

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
  const thread = $("thread");
  const card = thread.querySelector(".connect-card");
  if (!ready && !messages.length) {
    if (!card) thread.appendChild(connectCard());
  } else if (card) {
    card.remove();
  }
}

function connectCard() {
  const card = document.createElement("div");
  card.className = "connect-card";
  card.innerHTML =
    `<div class="cc-title">Connect your AI to start authoring</div>` +
    `<div class="cc-sub">Bring your own model — Anthropic (Claude), OpenAI, or any compatible endpoint. ` +
    `Your key stays in your browser; the engine never calls an LLM at runtime.</div>` +
    `<button type="button" class="primary cc-btn">Connect your AI</button>` +
    `<div class="cc-skill">Prefer your editor or a coding agent? The same draft→verify→repair loop is a portable ` +
    `<a href="https://github.com/maxgfr/feelc/tree/main/skills/feelc-rules" target="_blank" rel="noopener">feelc skill</a>.</div>`;
  card.querySelector(".cc-btn").addEventListener("click", openSettings);
  return card;
}

// toast shows a transient, non-blocking notification (replaces alert()); a11y-announced via #toasts.
function toast(msg, kind) {
  const wrap = $("toasts");
  if (!wrap) return;
  const t = document.createElement("div");
  t.className = "toast" + (kind ? " " + kind : "");
  t.textContent = msg;
  wrap.appendChild(t);
  setTimeout(() => { t.style.transition = "opacity .25s"; t.style.opacity = "0"; setTimeout(() => t.remove(), 260); }, 3200);
}

// ---- HTTP transport: the `api` shared.js + project.js call ----
async function api(path, opts) {
  const res = await fetch(path, opts);
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { raw: text }; }
  return { ok: res.ok, status: res.status, data };
}

// ---- chat ----
function renderMessage(content) {
  const parts = content.split(/```/);
  return parts.map((p, i) => {
    if (i % 2 === 1) {
      const body = p.replace(/^[a-zA-Z]*\n/, "");
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
function pushError(text, retry) {
  const div = document.createElement("div");
  div.className = "msg error";
  div.textContent = text;
  if (typeof retry === "function") {
    const b = document.createElement("button");
    b.type = "button"; b.className = "ghost retry"; b.textContent = "Retry";
    b.addEventListener("click", () => { div.remove(); retry(); });
    div.appendChild(document.createElement("br"));
    div.appendChild(b);
  }
  $("thread").appendChild(div);
  $("thread").scrollTop = $("thread").scrollHeight;
}

// pushThinking shows an animated "drafting…" placeholder while the LLM responds; returns a remover.
function pushThinking() {
  const div = document.createElement("div");
  div.className = "msg assistant thinking";
  div.innerHTML = `drafting<span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>`;
  div.setAttribute("aria-label", "drafting a reply");
  $("thread").appendChild(div);
  $("thread").scrollTop = $("thread").scrollHeight;
  return () => div.remove();
}

// applyDraft installs LLM-authored rules into the editor and kicks the live pipeline (auto-verify is
// gated by the checkbox; the graph + reactive simulator always refresh so the result is visible).
function applyDraft(rules) {
  $("source").value = rules;
  if ($("auto-verify").checked) refreshAll();
  else { showGraph(); rebuildSim(); }
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
  const stopThinking = pushThinking();
  // retry re-submits the same prompt (without duplicating it in the thread).
  const retry = () => { input.value = prompt; sendChat(new Event("submit")); };
  try {
    let spec = (typeof projectChatRequest === "function") ? projectChatRequest(messages, cfg) : null;
    if (!spec) spec = { path: "/v1/chat", body: JSON.stringify({ messages, llm: cfg }) };
    const { ok, status, data } = await api(spec.path, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: spec.body,
    });
    stopThinking();
    if (status === 501) {
      pushError("No LLM configured. Open ⚙ LLM settings to add a provider, model and API key.");
      $("settings").showModal();
      return;
    }
    if (!ok) { pushError(`Chat failed (${status}): ${data?.error || "unknown error"}`, retry); return; }
    pushMsg("assistant", data.message || "(empty reply)");
    if (data.rules) applyDraft(data.rules);
  } catch (err) {
    stopThinking();
    pushError("Network error: " + err.message, retry);
  } finally {
    btn.disabled = false; btn.textContent = "Send";
  }
}

// ---- ingest (spec -> draft -> verify -> bounded repair) ----
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
    if (data.rules) { $("source").value = data.rules; }
    clearReport();
    $("report").appendChild(renderIngestSteps(data.rounds, data.converged, data.blockers));
    ((data.verify && data.verify.findings) || []).forEach(renderFinding);
    await showTrace();
    await showGraph();
    await rebuildSim();
  } catch (err) {
    clearReport(); banner("err", "Network error: " + err.message);
  } finally {
    btn.disabled = false; btn.textContent = "Ingest";
  }
}

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

// ---- AI explain (narrate the deterministic trace of the last run) ----
async function explainLast() {
  const n = $("narration");
  if (!window.lastRun) { n.textContent = "Change an input first, then Explain."; return; }
  n.textContent = "…";
  const { ok, status, data } = await api("/v1/assist", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ task: "explain", payload: window.lastRun, llm: loadCfg() }),
  });
  if (status === 501) { n.textContent = "Configure your LLM (⚙) to use AI explanations."; return; }
  n.textContent = ok ? (data.message || "") : `explain failed (${status}): ${data?.error || ""}`;
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
function firstJSONArray(s) {
  const start = s.indexOf("[");
  const end = s.lastIndexOf("]");
  return start >= 0 && end > start ? s.slice(start, end + 1) : s;
}

// ---- settings ----
function openSettings() {
  const c = loadCfg();
  $("cfg-provider").value = c.provider || "anthropic";
  $("cfg-model").value = c.model || "";
  $("cfg-baseurl").value = c.baseURL || "";
  $("cfg-key").value = c.apiKey || "";
  const err = $("settings-err"); err.hidden = true; err.textContent = "";
  $("settings").showModal();
  $("cfg-model").focus();
}
function onSettingsClose() {
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
  $("explain-btn").addEventListener("click", explainLast);
  $("tests-btn").addEventListener("click", genTests);
  $("settings-btn").addEventListener("click", openSettings);
  $("llm-status").addEventListener("click", openSettings);
  // block "Save" with an empty model/key so the dialog can't silently store an unusable config.
  $("settings-form").addEventListener("submit", (e) => {
    if (!e.submitter || e.submitter.value !== "save") return;
    const model = $("cfg-model").value.trim(), key = $("cfg-key").value;
    if (model && key) return;
    e.preventDefault();
    const err = $("settings-err");
    err.textContent = !key ? "An API key is required." : "A model name is required.";
    err.hidden = false;
  });
  $("settings").addEventListener("close", onSettingsClose);
  $("chat-input").addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") $("chat-form").requestSubmit();
  });
  initSim(); // shared.js: reactive inputs ⇄ JSON ⇄ result, live on every edit
});

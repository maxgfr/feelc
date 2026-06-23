# ADR 0008 — AI authoring layer (optional, write-time, BYO-LLM, out of the core)

- **Status**: accepted (2026-06-22)
- **Deciders**: maxgfr

## Context

feelc's thesis is **"AI writes the rules, the engine executes them — no LLM in the core."** Until now
the AI step was external (a human driving Claude Code with the bundled `skill/`). We want the engine
to be *AI-driven from a web UI*: describe rules in natural language, have an LLM draft the `.rules`,
and let the deterministic engine compile/verify/run them — without ever putting an LLM on the
execution path.

## Decision

A thin, **optional** authoring layer at the HTTP boundary, with **two interchangeable paths**:

1. **In-UI chat (BYO-LLM)** — `feelc serve --ui` serves an embedded, zero-build vanilla UI
   (`internal/service/web/`, `//go:embed`). The user **configures their own LLM** (provider, model,
   base URL, API key) in a settings panel stored in the browser. A new package `internal/genai`
   (stdlib `net/http`, no SDK) speaks two protocols — **Anthropic** Messages API (default) and
   **OpenAI-compatible** Chat Completions (OpenAI/OpenRouter/local) — selected per request. The
   endpoint `POST /v1/chat` forwards the conversation (prepending the embedded authoring system
   prompt distilled from `skill/references/*` + `docs/feel-subset.md`) and returns the assistant
   message plus the extracted `.rules` block. The UI then drives the **deterministic** endpoints:
   `POST /v1/verify` (compile + verify a candidate, no swap) and `POST /v1/run` (evaluate a candidate
   + explain, no swap).

2. **Claude Code + `skill/`** — the existing power-user path: Claude Code drives the
   `interview → DSL → verify → run → iterate` flow using `feelc` (via `skill/scripts/feelc-skill.mjs`)
   as the deterministic oracle. Unchanged, no key handling in feelc.

**Separation of concerns (the invariant):** `internal/genai` is imported ONLY by the service's chat
handler. The engine packages (`compiler`, `ir`, `vm`, `verify`, `engine`) never import it and remain
pure, deterministic and network-free. The LLM influences *authoring*, never *execution*: every result
shown in the UI comes from the engine.

**Honest degradation:** no key in the request and none in the env (`ANTHROPIC_API_KEY` /
`FEELC_LLM_API_KEY`) ⇒ `POST /v1/chat` returns `501` with a clear message; the engine
(verify/run/decide) stays fully usable. Same ethos as the SMT backend (ADR 0007).

**Key handling / security:** the key is bring-your-own. In the UI path it is stored in the browser's
`localStorage` and sent per request to the **local** `feelc serve`, which forwards it to the chosen
provider. It is **not persisted nor logged** server-side. `feelc serve --ui` is a local/trusted-host
dev tool (CORS `*`); do not expose it publicly with a populated key. The default build remains
network-free — outbound calls happen only on an explicit `/v1/chat` request.

## Consequences

- New optional code paths (`internal/genai`, `/v1/chat`, `/v1/run`, embedded UI) with **zero new Go
  module dependencies** (stdlib only), CGO-free, single-binary preserved.
- The system prompt (`internal/genai/prompt/system.md`) must track the canonical subset
  (`docs/feel-subset.md`, `skill/references/*`); a unit test guards key tokens against drift.
- `feelc serve` may now start **without** `--rules` (when `--ui` is set): model-backed endpoints are
  `503` until a model is authored/loaded, while chat/verify/run-from-source work immediately.
- Determinism, reproducibility and auditability of execution are unchanged — the LLM is strictly a
  write-time assistant.

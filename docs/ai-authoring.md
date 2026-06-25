# AI authoring

feelc's thesis: **the AI writes the rules, the deterministic engine proves and runs them.** The LLM lives
only at the *authoring* boundary — drafting `.rules` text. Everything downstream (compile, verify, execute,
explain) is pure, reproducible Go that **never calls a model**. That separation is what makes AI authoring
safe to ship: the output is auditable, replayable, and identical every run (see
[ADR 0008](adr/0008-ai-authoring-layer.md)).

> The engine, not the model, decides when a rule is done. The LLM proposes; `verify` disposes.

## Bring your own LLM

There is no feelc-hosted model. You connect **your own** provider, model and key:

- **Anthropic** (Claude) or any **OpenAI-compatible** endpoint (`{provider, model, apiKey, baseURL?}`).
- In the UI the key is entered in **⚙ settings** and kept in your browser's `localStorage`; it transits
  only your local `feelc serve`, which forwards the request. It is never stored or logged server-side.
- Or set it in the environment: `ANTHROPIC_API_KEY`, or `FEELC_LLM_PROVIDER` / `FEELC_LLM_MODEL` /
  `FEELC_LLM_API_KEY` / `FEELC_LLM_BASE_URL`.
- **No key ⇒ the AI endpoints return `501`** and the rest of the engine keeps working fully. AI is
  additive, never required.

## Two interchangeable paths

### 1. The in-browser chat UI

```sh
feelc serve --ui            # then open http://localhost:8080/  (no --rules needed — author from scratch)
```

Describe your rules in plain English; your LLM drafts the `.rules`; one click runs `verify` / `run` on the
deterministic engine. The UI also renders the **decision graph**, builds a **simulator form** that asks only
the inputs a decision needs, narrates a result in **plain English** ("Explain"), and **generates test
cases** that are then checked deterministically. When no LLM is configured, the UI invites you to connect
one before the first request.

### 2. The portable skill (Claude Code, Codex, Cursor, …)

A self-contained skill lives in
[`skills/feelc-rules/`](https://github.com/maxgfr/feelc/tree/main/skills/feelc-rules). It guides a coding
agent through the **interview → write → verify → run → iterate** loop, using the `feelc` binary as a
**deterministic oracle** — the agent never decides a rule's outcome "in its head", it always runs feelc.

Install it through any of three channels:

```sh
npx skills add maxgfr/feelc                  # skills.sh → .claude/skills/feelc-rules
# inside Claude Code: /plugin marketplace add maxgfr/feelc  then  /plugin install feelc@feelc
feelc mcp install                            # wire the MCP server into ./.mcp.json
```

Or run the wrapper directly (it locates the `feelc` binary itself):

```sh
node skills/feelc-rules/scripts/feelc-skill.mjs verify --rules examples/credit/credit.rules --json
```

See [`skills/feelc-rules/SKILL.md`](https://github.com/maxgfr/feelc/blob/main/skills/feelc-rules/SKILL.md)
for the full flow, the supported subset, how to read the diagnostics, and the forbidden patterns.

## The red→green loop (`POST /v1/ingest`)

Both paths share the same convergence loop — drafting from an arbitrary specification, then **repairing
against the engine** until it proves out:

1. **Draft** — the LLM turns a spec (policy text, requirements, a contract clause — any domain) into
   `.rules`, with `@source` citations.
2. **Compile + verify** — the engine compiles the draft and runs `verify` (completeness, conflicts, dead
   rules) and optionally `check` (semantic claims).
3. **Count blockers** — only error-severity findings are *blockers*; warnings inform but don't gate.
4. **Repair** — the deterministic blockers, **with counterexample witnesses**, are fed back to the LLM for
   the next round.
5. **Converge** — repeat until zero blockers (bounded, default 3 rounds, max 5). **The engine decides when
   it's done** — and reports honestly if it stops with blockers remaining.

```sh
curl -s localhost:8080/v1/ingest -d '{
  "source": "Approve a loan when the credit score is at least 680 and debt-to-income is at most 0.43.",
  "maxRounds": 3
}' | jq '{converged, blockers, rounds}'
```

The response carries the per-round trace (`rounds: [{n, blockers, compileError}]`), the final `verify`
report, and the `@source` → decision mapping. The UI renders this as a `Draft → Round N → ✓ Converged`
stepper so the repair progression is visible at a glance.

## Authoring at project scale

In [project mode](project-mode.md), `POST /v1/project/chat` makes the chat **module-aware**: it builds a
**lexically-retrieved** context (the target module's source, the cross-module decisions it may bind to, and
the top-K other modules ranked by token overlap with your request — stdlib only, **no embeddings**,
char-capped) and hands it to your LLM. This keeps the prompt within the model's context window even across
hundreds of modules. As always, the LLM only drafts the module text; the engine compiles, verifies and
(with `--allow-edit`) persists it under the golden rule.

## What the AI never does

- It never executes a rule — `run` is the deterministic VM.
- It never decides completeness or a conflict — `verify` does, geometrically (and with SMT proofs under
  `-tags smt`).
- It never writes the traceability — `@source` mappings and explanation traces are read from the compiled
  IR, not from LLM prose.

That is the whole point: **AI for the writing, the engine for the truth.**

## Measuring authoring quality (`internal/eval`)

Because the engine is the oracle, authoring quality is **measurable**, not subjective. `internal/eval`
holds a frozen, solution-agnostic corpus of `prompt → reference cases` tasks and a **deterministic
`Score`**: a produced model is graded on whether it COMPILES, VERIFIES with zero blockers, and
REPRODUCES every reference case (`Result.OK()`). The LLM is the only nondeterminism in the loop; `Score`
itself is pure, so an authoring pipeline's first-try success rate and repair-rounds-to-green become real
numbers. The live driver plugs an LLM (via `POST /v1/ingest` / the MCP tools) in front of `Score`; the
scorer and corpus are regression-tested against reference solutions (`internal/eval/eval_test.go`).

## Agent integration (MCP)

Beyond the in-browser UI and HTTP API, `feelc mcp` exposes verify/run/explain/check/… as
[Model Context Protocol](mcp.md) tools, so any agent can drive the same red→green loop directly.

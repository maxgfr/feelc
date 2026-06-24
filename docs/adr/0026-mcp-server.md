# ADR 0026 — MCP server (`feelc mcp`)

- **Status**: Accepted (2026-06-24)
- **Deciders**: maxgfr

## Context

[ADR 0008](0008-llm-authoring-boundary.md) fixes feelc's AI stance: the LLM *writes* rules, the
deterministic engine *decides* outcomes — the model never sits on the execution path. That loop was
reachable two ways: the in-browser BYO-LLM UI and the `/v1/*` HTTP API. Neither is how coding agents
(Claude, Cursor, …) actually plug into tools today — they speak the **Model Context Protocol (MCP)**.
Market research (2026-06) confirmed that "AI rule authoring" itself is now commoditized
([comparison.md](../comparison.md)); feelc's defensible edge is that the engine *verifies* what the LLM
wrote. Exposing that verification as an MCP tool any agent can call turns the edge into a feature an
agent can actually use.

## Decision

Ship a first-class **MCP server** as `feelc mcp` (no extra binary): JSON-RPC 2.0 over stdio
(newline-delimited), implemented in `internal/mcp`. It exposes seven tools, each a thin wrapper over the
**same** core packages the CLI and WASM build use, so an MCP result is byte-identical to the
corresponding `feelc` command:

| Tool | Wraps | Purpose |
|------|-------|---------|
| `feelc_verify` | `loader.Compile` + `verify` | completeness/conflict/dead-rule findings + blockers |
| `feelc_run` | `engine.Run` | deterministic evaluation (exact decimals) |
| `feelc_explain` | `explain.Explain` | justification trace (which rule fired, cells) |
| `feelc_required` | `cm.RequiredInputs` | the inputs a decision needs (question-flow) |
| `feelc_check` | `check.Check` | verify NL claims against the model (red→green gate) |
| `feelc_graph` | `graph.Build` | DRG (Mermaid + DOT) + findings |
| `feelc_model` | `modelinfo` | the model surface (typed inputs + decisions) |

Design choices: input numbers are decoded with `UseNumber()` so decimals stay exact across the boundary;
a tool failure (e.g. a compile error) is returned as an **MCP tool error** (`isError:true`) carrying the
structured diagnostic — *not* a protocol error — so the agent sees the failure and can repair it (the
authoring loop). The LLM is still entirely outside `internal/mcp`; the server is pure, deterministic Go.

## Consequences

- Any MCP-capable agent can now author rules and have feelc verify/run/explain them with one config
  line (`feelc mcp` over stdio) — the verification moat becomes directly usable by agents, not just the
  bundled UI.
- No new runtime dependency; reuses the existing engine packages, so it inherits exact decimals,
  determinism, and the full supported surface (incl. ADR 0021–0025 features — regression-tested in
  `internal/mcp/mcp_test.go` against `power`, OUTPUT ORDER, bounded quantifiers, string predicates).
- Tests: `internal/mcp/mcp_test.go` (initialize, tools/list, tools/call happy-path + tool-error +
  unknown-method). See [docs/mcp.md](../mcp.md) for client configuration.

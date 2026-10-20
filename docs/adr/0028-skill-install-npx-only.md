# ADR 0028 — Install the skill with `npx skills` only; register the MCP server per agent

- **Status**: Accepted (2026-10-20)
- **Deciders**: maxgfr

## Context

[ADR 0027](0027-skill-distribution.md) shipped the `feelc-rules` skill through three channels, one of
them a Claude Code–specific plugin bundling the skill and the MCP server. `npx skills add maxgfr/feelc`
already installs the same skill into every supported agent (Claude Code, Codex, OpenCode, Cursor…) from
`skills/feelc-rules/SKILL.md`, and `npx skills update` keeps it current. Maintaining an agent-specific
package next to it duplicated the install story and its version metadata for one agent only.

## Decision

Drop the Claude Code plugin channel (ADR 0027, item 2) and remove its manifests. The skill installs with
`npx skills add maxgfr/feelc` only. Since `npx skills` does not configure MCP servers, the docs give the
per-agent registration of `feelc mcp`: `claude mcp add feelc -- feelc mcp`, `codex mcp add feelc -- feelc
mcp`, `opencode mcp add feelc -- feelc mcp` (or the `mcp.servers` JSON block), plus `feelc mcp install`
for `.mcp.json`. Keep the repo-root `.mcp.json`: it still serves contributors working in the repo.

## Consequences

- One generic install path for the skill; MCP wiring is one command per agent instead of being bundled
  for Claude Code alone.
- Users of the old plugin uninstall it and reinstall with `npx skills` (one-line note in the README).
- ADR 0027 items 1 and 3 (`skills/` layout, `feelc mcp install`) stand unchanged.

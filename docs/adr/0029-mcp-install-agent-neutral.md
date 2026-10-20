# ADR 0029 — `feelc mcp install` writes no agent-specific config

- **Status**: Accepted (2026-10-20)
- **Deciders**: maxgfr

## Context

[ADR 0027](0027-skill-distribution.md) item 3 added `feelc mcp install`, which merges the server entry
into `--target project|claude-desktop|claude-code`: a Claude Code project `.mcp.json` (the default), the
Claude Code user file `~/.claude.json`, or Claude Desktop's per-OS `claude_desktop_config.json`. Every
target belongs to one vendor's clients. [ADR 0028](0028-skill-install-npx-only.md) then made the skill
install agent-neutral (`npx skills add maxgfr/feelc`) and registered the MCP server per agent with each
agent's own command, but kept `feelc mcp install` "for `.mcp.json`". A default that writes one agent's
project file, and targets that know the private config paths of one vendor, contradict that: other agents
(Codex, OpenCode, …) use different formats and locations, and each agent's own `mcp add` command already
knows where its config lives.

## Decision

Make `feelc mcp install` agent-neutral:

- Remove `--target` and the Claude Code / Claude Desktop paths (`.mcp.json` default, `~/.claude.json`,
  `claude_desktop_config.json`). Passing `--target` is an error that points to `--path`.
- With no `--path`, write nothing: print the `mcpServers` JSON snippet on stdout and the per-agent
  commands on stderr (`claude mcp add feelc -- <feelc> mcp`, `codex mcp add …`, `opencode mcp add …`).
- Keep `--print` (the snippet alone) and `--path <file>`, which merges the entry into any `mcpServers`
  JSON file with the same idempotent, non-clobbering merge (`--force`, `--name`, `--command` unchanged).

This supersedes the `--target` part of ADR 0027 item 3 and the "`feelc mcp install` for `.mcp.json`"
clause of ADR 0028. The repo-root `.mcp.json` stays: it serves contributors working in the repo.

## Consequences

- feelc no longer knows any agent's private config location; agents own their config and are wired with
  their own `mcp add` command or a file the user names.
- Behaviour change: `feelc mcp install` with no flags no longer creates `./.mcp.json`; the equivalent is
  `feelc mcp install --path .mcp.json`. `--target claude-desktop` / `claude-code` users pass the file
  explicitly with `--path` or use the agent's command.
- `resolveConfigPath` / `claudeDesktopConfigPath` and their per-OS tests are gone; the merge helper keeps
  its table tests, and `cmd/feelc/mcp_install_test.go` covers `--path`, the write-nothing default, the
  printed commands and the rejected flags.

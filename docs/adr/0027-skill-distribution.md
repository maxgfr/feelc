# ADR 0027 — Skill distribution: skills.sh, Claude Code plugin, and `feelc mcp install`

- **Status**: Accepted (2026-06-25)
- **Deciders**: maxgfr

## Context

[ADR 0008](0008-llm-authoring-boundary.md) and [ADR 0026](0026-mcp-server.md) established feelc's AI
loop (the LLM writes, the engine decides) and exposed it to agents over MCP. But the **authoring skill**
that drives that loop was hard to actually install: it lived at `skill/` (singular), which is **not** a
path any skill registry scans, so `npx skills add maxgfr/feelc` could not find it — users had to know the
long `tree/main/skill` GitHub URL. Wiring the MCP server meant hand-editing `.mcp.json`. There was no
Claude Code plugin. Friction at the install step undercuts the whole "verification any agent can use"
thesis: a moat nobody can turn on is not a moat.

## Decision

Make the skill installable through **three first-class channels**, with the repository doubling as a
single-plugin Claude Code marketplace:

1. **skills.sh / `gh skill`** — relocate the skill to **`skills/feelc-rules/`**, the canonical flat
   layout (`skills/<name>/SKILL.md`) that registries discover first. `npx skills add maxgfr/feelc` now
   resolves it with no manifest or recursive-scan reliance. The portable `feelc-skill.mjs` binary-locator
   moved one level deeper, so its repo-root path math is `../../../` (the one load-bearing edit).
2. **Claude Code plugin** — add `.claude-plugin/marketplace.json` + `.claude-plugin/plugin.json` so the
   repo root *is* the plugin (`source: "./"`). Skills auto-discover from `skills/`; the MCP server
   auto-discovers from a bundled root **`.mcp.json`** (no inline `mcpServers`, avoiding conflicting
   manifests). `/plugin marketplace add maxgfr/feelc` then `/plugin install feelc@feelc` ships the skill
   **and** the MCP server together. Validated with `claude plugin validate --strict`.
3. **`feelc mcp install`** — a sub-subcommand of `feelc mcp` that merges `{"mcpServers":{"feelc":…}}`
   into a target config (`--target project|claude-desktop|claude-code`, `--print`, `--force`). The merge
   is **idempotent** and **non-clobbering** (preserves sibling servers and other keys) and points at the
   running binary's absolute path (`os.Executable()`), so the entry works even off-PATH.

The skill is **not** published to npm — distribution is registry/plugin/CLI, not a package install.

## Consequences

- One-liner install on every supported agent; the bundled root `.mcp.json` also means opening the feelc
  repo itself in Claude Code offers the MCP server automatically.
- The `feelc mcp install` merge logic is a pure, table-tested helper (`mergeMCPConfig` /
  `resolveConfigPath` in `cmd/feelc/mcp_install_test.go`): siblings preserved, idempotent, invalid-JSON
  rejected, per-OS Claude Desktop paths.
- Every doc reference to the old `skill/` path was updated (README, CONTRIBUTING, docs/ai-authoring,
  docs/mcp, docs/architecture, docs/comparison, `internal/genai` sync comments). Prior ADRs keep their
  historical `skill/` mentions (append-only).
- The exact Claude Code plugin manifest field names were validated against the live
  `claude plugin validate` tool rather than assumed.

# MCP server (`feelc mcp`)

feelc ships as a [Model Context Protocol](https://modelcontextprotocol.io) server so any MCP-capable
agent (Claude Code/Desktop, Cursor, …) can **author rules and have the deterministic engine verify, run
and explain them** — the LLM writes, feelc decides ([ADR 0008](adr/0008-llm-authoring-boundary.md),
[ADR 0026](adr/0026-mcp-server.md)). The model never sits on the execution path; every tool is pure,
deterministic Go that reuses the same code as `feelc run`/`verify`/….

## Run it

```bash
feelc mcp        # speaks JSON-RPC 2.0 over stdin/stdout (newline-delimited); no flags
```

## Configure a client

**One command** — `feelc mcp install` writes/merges the config for you, idempotently (it never clobbers
other servers or sibling keys, and points at this binary's absolute path):

```bash
feelc mcp install                          # → ./.mcp.json (Claude Code project scope)
feelc mcp install --target claude-desktop  # → the per-OS claude_desktop_config.json
feelc mcp install --print                  # just print the JSON snippet, write nothing
```

Or add it by hand — Claude Desktop / Code (`claude_desktop_config.json` or `.mcp.json`):

```json
{
  "mcpServers": {
    "feelc": { "command": "feelc", "args": ["mcp"] }
  }
}
```

Any client that launches a stdio MCP server works the same way — point it at the `feelc` binary with the
`mcp` argument. Installing the [Claude Code plugin](https://github.com/maxgfr/feelc) (`/plugin install
feelc@feelc`) wires this server up automatically.

## Tools

Every tool takes the `.rules` DSL source as `rules`; the engine is the oracle.

| Tool | Arguments | Returns |
|------|-----------|---------|
| `feelc_verify` | `rules` | `{hash, findings, blockers}` — `blockers==0` ⇒ buildable |
| `feelc_run` | `rules, decision, input` | `{decision, output}` (exact decimals) |
| `feelc_explain` | `rules, decision, input` | justification trace (winning rule + cells) |
| `feelc_required` | `rules, decision` | `{decision, inputs}` — only ask the user for these |
| `feelc_check` | `rules, claims[]` | per-claim verdict (supported/contradicted/error) + blockers |
| `feelc_graph` | `rules` | `{mermaid, dot, findings}` |
| `feelc_model` | `rules` | `{name, inputs, decisions}` — the model surface |

A tool that fails (e.g. a compile error) returns an MCP **tool error** carrying the structured
diagnostic (`{file, line, col, code, message, suggestion}`), so the agent can read it and repair — the
red→green authoring loop.

## Quick check

```bash
printf '%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | feelc mcp
```

This prints the server handshake and the seven tool descriptors.

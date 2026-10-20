# MCP server (`feelc mcp`)

feelc ships as a [Model Context Protocol](https://modelcontextprotocol.io) server so any MCP-capable
agent (Claude Code, Codex, OpenCode, Cursor, …) can **author rules and have the deterministic engine verify, run
and explain them** — the LLM writes, feelc decides ([ADR 0008](adr/0008-llm-authoring-boundary.md),
[ADR 0026](adr/0026-mcp-server.md)). The model never sits on the execution path; every tool is pure,
deterministic Go that reuses the same code as `feelc run`/`verify`/….

## Run it

```bash
feelc mcp        # speaks JSON-RPC 2.0 over stdin/stdout (newline-delimited); no flags
```

## Configure a client

Each agent owns its config format and location, so feelc does not write into any one agent's files.
`feelc mcp install` prints what to add and how ([ADR 0029](adr/0029-mcp-install-agent-neutral.md)); with
`--path` it merges the entry into a file you name, idempotently (it never clobbers other servers or sibling
keys, and points at this binary's absolute path):

```bash
feelc mcp install                       # print the mcpServers JSON + the per-agent commands, write nothing
feelc mcp install --print               # just the JSON snippet
feelc mcp install --path .mcp.json      # merge it into any mcpServers JSON file (--force to overwrite)
```

Or add it by hand to any client that reads an `mcpServers` JSON file:

```json
{
  "mcpServers": {
    "feelc": { "command": "feelc", "args": ["mcp"] }
  }
}
```

Any client that launches a stdio MCP server works the same way — point it at the `feelc` binary with the
`mcp` argument. Per-agent one-liners:

```sh
claude mcp add feelc -- feelc mcp      # Claude Code
codex mcp add feelc -- feelc mcp       # Codex (~/.codex/config.toml)
opencode mcp add feelc -- feelc mcp    # OpenCode (--global for ~/.config/opencode/opencode.json)
```

OpenCode by hand (`opencode.json`, `.opencode/opencode.json` or `~/.config/opencode/opencode.json`):

```json
{ "mcp": { "servers": { "feelc": { "type": "local", "command": ["feelc", "mcp"] } } } }
```

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

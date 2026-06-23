# CLI reference

The `feelc` binary has one subcommand per task. Run `feelc help` for the synopsis, `feelc version` for the
build version. Commands that take a model accept **either** a `.rules` source **or** a compiled `.ir.bin`
(detected automatically) unless noted; `compile`, `fmt` and `export` read `.rules` source only, `import`
reads `.dmn`.

Every command returns a **non-zero exit code on failure** (compile/parse error, or the per-command
condition below); `--json` (where available) emits machine-readable output, and a structured `diag.Error`
on a compile error (see [error schema](error-schema.md)).

## Authoring & evaluation

| Command | Flags | Notes |
|---|---|---|
| `feelc run` | `--rules <f>` · `--decision <name>` · `--input '<json>'` (default `{}`) · `--json` | Evaluate one decision. Prints the value (and unit, if any). |
| `feelc explain` | `--rules <f>` · `--decision <name>` · `--input '<json>'` · `--json` | Justification trace: winning rule + contributing cells with source lines. |
| `feelc inputs` | `--rules <f>` · `--decision <name>` · `--json` | The inputs a decision transitively needs (question-flow / simulator). |
| `feelc compile` | `--rules <f>` · `-o <model.ir.bin>` · `--json` | Compile `.rules` → canonical IR; prints the model hash on stderr. |
| `feelc fmt` | `--rules <f>` · `-w` (rewrite) · `--check` (exit≠0 if unformatted) | Canonical pretty-printer. Does **not** preserve comments or the `model { … }` body. |

## Verification

| Command | Flags | Notes |
|---|---|---|
| `feelc verify` | `--rules <f>` · `--json` | Completeness / conflicts / dead rules / subsumption, with counterexamples. **Exit≠0 if any blocker.** |
| `feelc check` | `--rules <f>` · `--claims <claims.json>` · `--json` | Semantic gate: NL claims vs engine verdicts. **Exit≠0 if any claim is unsupported.** |
| `feelc tck` | `--suite <dir>` · `--json` · `--min <pct>` | DMN TCK conformance run. **Exit≠0 on any failure, or below `--min`.** |

## Interop & docs

| Command | Flags | Notes |
|---|---|---|
| `feelc import` | `--in <model.dmn>` · `-o <out.rules>` | DMN 1.3 XML → `.rules` (warnings on stderr). |
| `feelc export` | `--rules <f.rules>` · `-o <out.dmn>` | `.rules` → DMN 1.3 XML. |
| `feelc graph` | `--rules <f>` · `--format mermaid\|dot\|json` (default `mermaid`) · `-o <file>` | Decision-requirements graph with findings overlaid. |
| `feelc docs` | `--rules <f>` · `-o <DOC.md>` · `--trace` · `--spec <file>` (implies `--trace`) | Markdown reference (inputs/decisions/Mermaid graph), optional source↔rule traceability. |

## Service

```sh
feelc serve --rules <f.rules> [--addr :8080] [--watch] [--strict] [--ui]
feelc serve --project <dir>   [--addr :8080] [--watch] [--strict] [--ui] [--allow-edit]
feelc serve --ui              # start empty; author a model in the browser
# opt-in hardening (any mode):  [--auth-token <tok>]  [--rate-limit <rps>]
```

`--rules` and `--project` are mutually exclusive. `--watch` hot-reloads on file change; `--strict` refuses
to (re)load a model with verification blockers; `--ui` serves the embedded authoring UI at `/`;
`--allow-edit` enables the project module-write endpoints (trusted/loopback hosts only). See the
[HTTP API reference](http-api.md) for the routes. With `--ui`, configure your LLM in the browser or via the
`ANTHROPIC_API_KEY` / `FEELC_LLM_*` environment variables (no key ⇒ AI endpoints return `501`).

For exposed deployments, the server is hardened **opt-in** (default = open, loopback-only): `--auth-token`
(or `FEELC_AUTH_TOKEN`) requires `Authorization: Bearer <tok>` on every route except the health probes
(missing/invalid ⇒ `401`); `--rate-limit <rps>` token-buckets requests per client IP (`429` over budget).
The server also shuts down gracefully on `SIGINT`/`SIGTERM` (draining in-flight requests).

## Environment

| Variable | Effect |
|---|---|
| `FEELC_MEMLIMIT` | Process memory ceiling in bytes (default 2 GiB; `GOMEMLIMIT` takes priority). |
| `ANTHROPIC_API_KEY` / `FEELC_LLM_API_KEY` | Default LLM credential for `serve --ui` (overridable per request). |

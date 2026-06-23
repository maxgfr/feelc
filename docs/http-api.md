# HTTP API reference

`feelc serve` exposes the engine over HTTP (`net/http`). This is the complete route table. Conventions:

- Request bodies are JSON unless noted "raw `.rules`"; every body is capped at **8 MiB** (`413` beyond).
- **CORS** is reflected only for loopback origins — the API proxies the user's LLM key, so it is a
  local / trusted-host tool (the write endpoints are off unless `serve --project --allow-edit`).
- Common status codes: `422` a compile error (structured [`diag.Error`](error-schema.md) body) or an
  evaluation error; `503` no model loaded yet; `501` no LLM configured; `404` route not available in the
  current mode; `502` an upstream LLM call failed.
- **Opt-in hardening** (default off): with `serve --auth-token`, every route except `/healthz`/`/readyz`
  requires `Authorization: Bearer <tok>` (`401` otherwise); with `serve --rate-limit`, requests are capped
  per client IP (`429` over budget). Every response carries an `X-Request-ID` (echoing a supplied one).

## Decisions on the served model

| Method & path | Body | Response |
|---|---|---|
| `POST /v1/decisions/{key}` | input object | `{decision, output, modelVersion, hash, durationNs}` |
| `POST /v1/decisions/{key}/explain` | input object | justification trace |
| `POST /v1/evaluate` | `{decision, input}` (decision falls back to the project `default`) | as `decisions/{key}` |
| `GET /v1/model` | — | `{name, version, hash, inputs, decisions}` |
| `GET /v1/source` | — | current `.rules` text (`404` if loaded from `.ir.bin`) |

## Candidate (compile-from-body, no swap)

These compile a model sent in the request and return analysis **without** changing the served model — the
web editor / playground use them. Compilation is memoized by source hash.

| Method & path | Body | Response |
|---|---|---|
| `POST /v1/verify` | raw `.rules` | `{hash, report, blockers}` |
| `POST /v1/run` | `{rules, decision, input, explain?, full?}` | `{decision, output, trace?}` |
| `POST /v1/check` | `{rules, claims}` | `{report, blockers}` |
| `POST /v1/graph` | raw `.rules` | `{mermaid, dot, graph, findings, blockers}` |
| `POST /v1/trace` | raw `.rules` | source↔rule traceability + coverage |
| `POST /v1/required` | `{rules, decision}` | `{decision, inputs}` (question-flow) |

## AI authoring (bring-your-own LLM)

The LLM is reached only here; the engine never calls it. Each takes an optional `llm` config
(`{provider, model, apiKey, …}`) or falls back to env. `501` when no LLM is configured.

| Method & path | Body | Response |
|---|---|---|
| `POST /v1/chat` | `{messages, llm}` | `{message, rules}` (NL → `.rules` draft) |
| `POST /v1/ingest` | spec + options | verify→repair→converge loop result |
| `POST /v1/assist` | `{task: "explain"\|"tests", payload, llm}` | `{message, rules}` |

## Project (multi-module) — `serve --project`

`404` outside project mode. Write endpoints (`PUT`/`POST`/`DELETE`) also require `--allow-edit`.

| Method & path | Body | Response |
|---|---|---|
| `GET /v1/project` | — | manifest summary + module list |
| `GET /v1/project/health` | — | aggregated verification report |
| `GET /v1/project/graph` | — | cross-module decision graph |
| `POST /v1/project/verify` | `{name, modules}` | `{hash, status, report, blockers}` (incremental reuse) |
| `POST /v1/project/chat` | `{messages, module, llm}` | project-aware `.rules` draft (lexical retrieval) |
| `GET /v1/modules` | — | per-module summary |
| `GET /v1/modules/{name}/source` | — | a module's `.rules` source |
| `PUT /v1/modules/{name}/source` | raw `.rules` | edit + persist (golden rule) |
| `POST /v1/modules` | `{name, source}` | create a module |
| `DELETE /v1/modules/{name}` | — | delete a module (rejected if bound by another) |

## Admin & health

| Method & path | Purpose |
|---|---|
| `POST /v1/admin/reload` | re-read the model/project from disk (`501` if reload unavailable) |
| `GET /v1/stats` | candidate-compile cache hit rate + project size (observability, JSON) |
| `GET /metrics` | request + cache counters in Prometheus text format |
| `GET /healthz` | liveness (`ok`) |
| `GET /readyz` | readiness (`ready`, or `503` until a model is loaded) |
| `GET /` | the embedded authoring UI (only with `--ui`) |

# Project mode

A single `.rules` file is one model. A **project** is a directory of `.rules` *modules* plus an optional
`feelc.project.json` manifest, compiled and **linked into one deterministic model** that the engine runs
unchanged — one hash, one verification pass, one decision graph. This is how feelc scales from one model
to a portfolio of hundreds of rules.

> Project mode never puts an LLM on the execution path and never changes how a model executes: the merged
> model is an ordinary compiled model, reproducibly hashed and run by the same deterministic VM. Serving a
> lone `.rules` file as a one-module project is the identity transform — its hash is unchanged.

## Directory layout

```
myproject/
  feelc.project.json     # optional manifest (absent → every *.rules is auto-discovered)
  kyc.rules
  pricing.rules
  loan.rules
```

Serve it:

```sh
feelc serve --project myproject [--addr :8080] [--watch] [--strict] [--ui]
```

`--project` is mutually exclusive with `--rules`. With no manifest, each `*.rules` file becomes a module
named after its file stem.

## Manifest — `feelc.project.json`

```json
{
  "name": "lending",
  "version": "0.1.0",
  "modules": [
    { "name": "kyc",     "path": "kyc.rules" },
    { "name": "pricing", "path": "pricing.rules" },
    { "name": "loan",    "path": "loan.rules", "uses": { "kyc_ok": "kyc.passed" } }
  ],
  "default": "loan__approved",
  "tags": ["demo", "lending"]
}
```

| Field      | Meaning                                                                              |
|------------|--------------------------------------------------------------------------------------|
| `name`     | project name (defaults to the directory name)                                        |
| `modules`  | explicit module list; ordering does not affect the project hash (modules are sorted) |
| `…​.uses`  | cross-module bindings — a local input wired to `othermodule.decision` (see below)     |
| `default`  | the (qualified) decision used by a bare `POST /v1/evaluate`                           |

Module names may not contain `.`, `/`, `\`, whitespace or `__` (the reserved namespace separator).

## Namespacing

Every name is qualified to `module__name` in the merged model, so modules never collide — two modules can
both declare `age` and they stay distinct (`credit__age`, `insurance__age`). You address a decision or
input by its qualified name:

```sh
curl -s localhost:8080/v1/decisions/loan__approved \
  -d '{"kyc__score":700,"loan__amount":50000}'
```

## Cross-module references — `uses`

A module references another module's decision through a manifest `uses` binding. Because each module also
compiles **standalone**, the referenced value is declared as a normal `input`; the manifest then wires it:

```json
{ "name": "loan", "path": "loan.rules", "uses": { "kyc_ok": "kyc.passed" } }
```

At link time `loan`'s `kyc_ok` input is replaced by a dependency on `kyc__passed`, so `loan__approved`
transitively needs only the real external inputs (`kyc__score`, `loan__amount`). The dot lives only in the
JSON manifest, never in a `.rules` cell. Dangling bindings and cross-module dependency cycles are rejected
at load.

## Verification & health

Each module is verified on load. `GET /v1/project/health` aggregates the findings into a report — per
module gap / conflict / dead-rule counts, an overall status (`clean` / `warnings` / `blocked`), and
cross-module advisories (e.g. the same input name declared independently in two modules). The cross-module
decision graph is at `GET /v1/project/graph`.

## Editing & persistence (with `--ui --allow-edit`)

`feelc serve --project <dir> --ui` adds, to the authoring UI, a left-rail **module navigator** (with health
dots), a health dashboard, and the cross-module graph. By default this is **read-only**. Adding
**`--allow-edit`** enables the per-module editor's server-side **Save** plus module create/delete — the
mutating endpoints `PUT/POST/DELETE /v1/modules` that write to disk. Edits then persist back to the
directory, and every mutation follows the **golden rule**: the whole project is recompiled and verified
first, and only written + swapped if it links — an invalid edit is rejected and the live project is kept.
`--watch` additionally hot-reloads external file changes (independently of `--allow-edit`).

> **Safe by default.** The write endpoints are off unless `--allow-edit` is passed, request bodies are size
> capped, and the service has no authentication — so `--allow-edit` (like `--ui`) is for a **trusted /
> loopback host only**. Bind to `127.0.0.1` (or sit behind an authenticating proxy) before exposing it.

## AI authoring at project scale

With `--ui` the chat panel becomes **project-aware**: when a module is selected, a message is sent to
`POST /v1/project/chat`, which builds a **lexically-retrieved** context (no embeddings) — the target
module's source, the cross-module decisions it may bind to, and the top-K other modules ranked by token
overlap with your request — and hands it to your configured LLM. This keeps the prompt within the model's
context window even for projects with hundreds of rules. As always, the LLM only drafts `.rules` text; the
deterministic engine compiles, verifies and (with `--allow-edit`) persists it under the golden rule.

## HTTP API (project endpoints)

| Method + path                       | Purpose                                                        |
|-------------------------------------|---------------------------------------------------------------|
| `GET /v1/project`                   | manifest summary + module list (404 in single-file mode)      |
| `GET /v1/project/health`            | aggregated verification report                                |
| `GET /v1/project/graph`             | cross-module decision-requirements graph                      |
| `POST /v1/project/verify`           | verify a candidate project from the body (no swap)            |
| `POST /v1/project/chat`             | project-aware AI authoring: edit a module with retrieved context |
| `GET /v1/modules`                   | per-module summary                                            |
| `GET /v1/modules/{name}/source`     | a module's `.rules` source                                    |
| `PUT /v1/modules/{name}/source`     | edit + persist a module (golden rule)                         |
| `POST /v1/modules`                  | create a module `{name, source}`                              |
| `DELETE /v1/modules/{name}`         | delete a module (rejected if another module binds to it)      |
| `GET /v1/stats`                     | compile-cache hit rate + project size (observability; global) |

The single-model endpoints (`/v1/decisions/{key}`, `/v1/model`, `/v1/verify`, …) work unchanged on the
merged model; the [HTTP API reference](http-api.md) is the complete route table. The mutating `PUT/POST/DELETE /v1/modules` endpoints are enabled only with `--allow-edit`
(otherwise they 404), have browser CORS restricted to loopback origins, and every request body is size
capped — but there is no authentication, so the editing surface is a local / trusted-host tool.

## Docker

The engine is CGO-free, so it ships as a single static binary on a distroless **nonroot** base. The
default container is **read-only**:

```sh
docker build -t feelc .
docker run --rm -p 8080:8080 -v "$PWD/myproject:/work" feelc            # read-only navigator + dashboard
```

To enable in-browser editing on a trusted machine, bind to loopback and add `--allow-edit` (and make
`/work` writable by uid 65532):

```sh
docker run --rm -p 127.0.0.1:8080:8080 -v "$PWD/myproject:/work" \
  feelc serve --project /work --addr :8080 --ui --watch --allow-edit
```

Mount your project at `/work`; edits made in the UI persist back to the volume. See
[`sample-project/`](https://github.com/maxgfr/feelc/tree/main/sample-project) for a runnable example, and
ADR 0015 for the design rationale.

# Sample project — `lending`

A small multi-module feelc **project**: several `.rules` modules compiled and linked into one
deterministic model, served from a single directory. It demonstrates the three things project mode adds
on top of single-file `.rules`:

| Module        | Shows                                                                       |
|---------------|-----------------------------------------------------------------------------|
| `kyc`         | a reusable decision (`passed`) other modules bind to                        |
| `pricing`     | an independent module: a decision table with a cross-column cell + a context output |
| `loan`        | a **cross-module binding** — its `kyc_ok` input is wired to `kyc.passed`    |

Every name is namespaced under `module__name` in the merged model, so modules never collide
(e.g. `kyc__score`, `loan__amount`, `pricing__tier`). The binding is declared in
[`feelc.project.json`](./feelc.project.json):

```json
{ "name": "loan", "path": "loan.rules", "uses": { "kyc_ok": "kyc.passed" } }
```

so `loan__approved` transitively depends on `kyc__passed` — provide only the real external inputs and
the engine computes the rest.

## Run it

```sh
# locally (read-only navigator + dashboard)
feelc serve --project sample-project --ui --watch
# open http://localhost:8080/ — the left rail lists the modules.

# to edit + Save to disk, add --allow-edit (trusted/loopback host only — the surface is unauthenticated)
feelc serve --project sample-project --ui --watch --allow-edit

# or in Docker (from the repo root) — the default image is read-only and runs as a nonroot user
docker build -t feelc .
docker run --rm -p 8080:8080 -v "$PWD/sample-project:/work" feelc
```

Evaluate the linked decision (external inputs only — `loan__kyc_ok` is satisfied by `kyc__passed`):

```sh
curl -s localhost:8080/v1/decisions/loan__approved \
  -d '{"kyc__score":700,"loan__amount":50000}'        # => {"output": true, ...}

curl -s localhost:8080/v1/decisions/pricing__tier \
  -d '{"pricing__amount":100,"pricing__threshold":50}' # => {"label":"high","surcharge":10}
```

## Note on the health badge

The project health shows **warnings**, not blocked: `pricing__tier`'s cross-column cell (`>= threshold`)
is an expression cell that the geometric verifier honestly reports as *not provable* in the default
build. Building with the optional SMT backend (`-tags smt`, ADR 0007) discharges it.

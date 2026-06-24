# Decisions

The key engineering decisions behind feelc, one line each. Each links to its full Architecture Decision
Record (the detailed context + consequences live in [`docs/adr/`](https://github.com/maxgfr/feelc/tree/main/docs/adr)).

| # | Decision | Why |
|---|----------|-----|
| [0001](adr/0001-feel-frontend.md) | Reuse a vendored FEEL parser fork, not an in-house parser | Chosen after a measured spike; less code to own, same coverage. |
| [0002](adr/0002-decimal.md) | Exact decimal arithmetic (`apd`), never floats | Money/tax rules must be bit-for-bit exact and reproducible. |
| [0003](adr/0003-null-error-semantics.md) | Three-valued null/error semantics | No silent coercion — out-of-scope inputs fail honestly. |
| [0004](adr/0004-deferrals.md) | Record deferred features explicitly | A deferral is documented, not hidden (temporal since lifted). |
| [0005](adr/0005-structured-errors.md) | Positioned, coded, JSON-serializable diagnostics | The contract the authoring loop reads to fix sources. |
| [0006](adr/0006-ir-serialization.md) | Canonical hashed binary IR, hardened against untrusted blobs | The model hash is the identity; decoding must be safe. |
| [0007](adr/0007-smt-backend.md) | Optional Z3/SMT verification behind a build tag | Proves the non-geometric cells geometry can't; honest fallback. |
| [0008](adr/0008-ai-authoring-layer.md) | LLM authors at the boundary, never in the core | "AI writes, the engine executes" — deterministic, auditable. |
| [0009](adr/0009-graph-visualization.md) | Render the decision-requirements graph | See the model and its verification findings, not just run it. |
| [0010](adr/0010-rule-metadata.md) | `@title/@doc/@question/@source` annotations | Documentation + law/source traceability, hash-neutral. |
| [0011](adr/0011-progressive-brackets.md) | `bracket:` lowered to plain arithmetic | Marginal-rate schedules with no special runtime op. |
| [0012](adr/0012-units.md) | Compile-time dimensional analysis | Reject dimensionally-inconsistent arithmetic (units & money). |
| [0013](adr/0013-applicability.md) | Non-applicable values via a sentinel | Eligibility gating with no VM special-case. |
| [0014](adr/0014-temporal-types.md) | Whole-day `date` & `duration` arithmetic | Sound, exact calendar logic without time-of-day complexity. |
| [0015](adr/0015-project-mode.md) | Multi-module projects linked into one model | Manage many rules; one hash, one verification pass. |
| [0016](adr/0016-canonical-language-english.md) | English is canonical | Error strings are a test contract; one shared vocabulary. |
| [0017](adr/0017-wasm-playground.md) | Compile the real engine to WebAssembly | Run it in the browser with zero setup, no second engine. |
| [0018](adr/0018-docs-site.md) | Zero-dependency docs-site generator | Publish docs + decisions without an SSG toolchain. |
| [0019](adr/0019-embeddable-engine-package.md) | Ship the engine as the `@feelc/engine` npm package | Embed the real engine in any TS app (browser/Node/bundler/edge), no API. |
| [0020](adr/0020-deterministic-extra-builtins.md) | Deterministic extra built-ins: `round(x,n)`, `abs`, `trunc`, `modulo` | Close near-universal gaps vs other rule engines without weakening determinism/verification. |

ADRs are append-only: an accepted decision is never rewritten — a later change is a dated note on the same
ADR or a new superseding one.

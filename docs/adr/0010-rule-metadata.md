# ADR 0010 — Rule metadata & law/source traceability

- **Status**: accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

Rule engines often attach `title`/`description`/`question` metadata to rules (driving documentation and
interactive simulators) and tie each rule to the legal article it implements (a literate-programming
style). feelc had neither: rules were computation-only. This blocks
auto-generated docs, an interactive question-flow, and audit traceability ("which law does this
encode?").

## Decision

Add optional, line-oriented annotations before any `input` or `decision`:

```
@title    "short label"
@doc      "longer description"
@question "prompt shown when asking for this input"
@source   "Labor Code, Art. L1225-35"
input months_employed : number >= 0
```

The parser accumulates pending annotations and attaches them to the next input/decision; a dangling
annotation (not immediately followed by one) is a loud error. They flow `model.Meta` → `ir.Meta`
(`Decision.Meta`, `CompiledModel.InputMeta`) and surface in: the `explain` trace (`title`/`source`),
`GET /v1/model` (inputs + decisions), the graph node labels, and generated docs (ADR 0009 / Phase 5).

**Descriptive, not computational.** Metadata is **excluded from the canonical encoding and hash**
(ADR 0006): two models differing only by documentation are the same computational model and share a
hash. Consequently it is **dropped on `.ir.bin` serialization** (the codec is untouched) — docs live
with the source. Note that source *line numbers* remain part of the encoding (existing behavior), so
adding annotation lines shifts line-sensitive hashes of that source, as expected.

## Consequences

- New optional syntax; fully backward-compatible (existing models parse unchanged).
- Enables Phase 3 (question-flow uses `@question`), Phase 5 (docs embed `@source`/`@doc`), and richer
  `explain`/graph — the AI-driven, transparent rule-engine story.
- Law/source traceability without adopting full literate programming.

# ADR 0016 — Canonical language: English

- **Status**: Accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

feelc was originally authored with French source comments, error messages, examples and docs. As the
project opened up (public repo, docs site, an authoring skill used by LLM agents), the mixed language was
a barrier, and — more subtly — the **user-facing error strings are a test contract**: goldens and
diagnostic assertions match on exact message substrings. Commit `8dfd34f` translated ~78 files to English
and regenerated the goldens, establishing a glossary that maps each former diagnostic to one canonical
English phrasing (for example the diagnostic prefix became `line N:` and the FEEL-cell error became
`invalid FEEL cell ...`). This is a structuring, cross-cutting decision that the project's own rule
([CONTRIBUTING](../../CONTRIBUTING.md)) requires to be recorded.

## Decision

**English is the canonical language** for source comments, diagnostic/error messages, examples, the DSL
keywords already being English, and all documentation. The error-message glossary is treated as a stable
vocabulary: a given condition maps to one English phrasing, reused everywhere it is asserted.

## Consequences

- Changing a user-facing error/diagnostic string is a **golden-affecting change**: regenerate the goldens
  (`FEELC_REGEN_GOLDEN=1 go test ./internal/engine -run Golden`) and update any substring assertion.
- New messages must follow the existing glossary rather than inventing synonyms, so test assertions and
  the skill's expectations stay stable.
- No localization/i18n layer is in scope; the `internal/i18nguard` test exists only to keep the surface
  consistent, not to support multiple languages.

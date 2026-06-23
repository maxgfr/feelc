# ADR 0018 — Docs site & ADR publication

- **Status**: Accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

feelc needs a documentation site (GitHub Pages) that hosts the reference docs and the WASM playground, and
publishes the decision records. A full static-site generator (Jekyll/Hugo/MkDocs) would add a toolchain and
dependency surface out of proportion with a handful of markdown pages, and risks the site drifting from the
repo's `docs/*.md`. The repo already uses build-time Node tooling (release flow), so a tiny generator fits.

## Decision

Render the site with a **zero-dependency Node script**, `site/build.mjs`, whose only
build-time dependency is `marked` (installed `--no-save`, not committed). `docs/*.md` is the single source
of the reference pages (a `DOCS` array) and **`docs/adr/*.md` is auto-discovered** (excluding `0000-*` and
`README`) and published behind a single *Decision records* index page — contributor rationale kept out of
the user reference nav. `.github/workflows/pages.yml` builds the `.wasm` ([ADR 0017](0017-wasm-playground.md))
and deploys `site/`.

**ADR lifecycle (canonical statement).** ADRs are **append-only**: an accepted `## Decision` is never
rewritten. A non-reversing follow-up is a dated `## Update` addendum on the same ADR (annotate the Status
with `amended YYYY-MM-DD`); a reversal is a new *superseding* ADR, and the superseded one's Status flips to
`Superseded by ADR NNNN`. The site index renders this policy; the full convention + template live in
[`docs/adr/README.md`](README.md).

## Consequences

- Adding a reference page = one `DOCS` entry; adding an ADR = one file — the nav and ADR index update
  automatically, so docs cannot silently drift from the repo.
- The generator is intentionally minimal (themed HTML, a sidebar, `.md`→`.html` link rewriting, a relative
  link check); it is not a general SSG and gains features only as the site needs them.
- The decision lifecycle now has a single authoritative home (this ADR + `docs/adr/README.md`), replacing
  the earlier, contradictory "strictly immutable" wording.

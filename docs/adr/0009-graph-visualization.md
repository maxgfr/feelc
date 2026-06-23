# ADR 0009 — Decision Requirements Graph (DRG) visualization

- **Status**: accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

feelc already computes a topologically-sorted decision graph (`Decision.Deps`, `internal/ir/ir.go`)
and a verification report, but had no way to *see* it. The need to understand "what feeds what"
(and where the gaps/conflicts are) calls for a first-class graph
view — a headline feature of an AI-driven rule engine.

## Decision

A pure `internal/graph` package builds the DRG from a compiled model + a `verify.Report` (no new
analysis — it reuses `Deps` and the findings) and renders it three ways:

- **Mermaid** and **DOT** (text, for docs/external tools and `feelc graph`),
- **JSON** (consumed by the UI).

Inputs are stadium nodes, decisions are rectangles labelled with their hit policy; verification
findings are **overlaid** (worst severity colours the node, messages become tooltips).

- **CLI**: `feelc graph --rules <m> [--format mermaid|dot|json] [-o file]`.
- **Service**: `POST /v1/graph` compiles a candidate source (no swap, like `/v1/verify`) and returns
  all renderings + findings.
- **UI**: a "Graph" button renders the DRG with a **built-in, zero-dependency SVG layered layout**
  (rank 0 = inputs; a decision's rank = 1 + max rank of its dependencies). We deliberately do **not**
  vendor a heavy JS graph library (e.g. mermaid ~3 MB): the in-browser renderer keeps the single
  binary small and fully offline, consistent with the project's zero-dependency ethos. The
  Mermaid/DOT text outputs remain available for anyone who wants richer external rendering.

## Consequences

- New read-only surface; no engine change, no new Go module dependency, determinism untouched.
- The graph is honest: a dependency that is neither a declared input nor a decision is surfaced as a
  node rather than silently dropped.
- Foundation for Phase 3 (question-flow uses the same `Deps` reachability) and Phase 5 (generated
  docs embed the Mermaid DRG).

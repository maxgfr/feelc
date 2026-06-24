# ADR 0025 — Bounded quantifiers `every/some of {…} satisfies ?`

- **Status**: Accepted (2026-06-24)
- **Deciders**: maxgfr

## Context

"Every dependant is under 26", "some line item exceeds the cap", "all three scores pass" — these are
everyday rules that need a quantifier. Native FEEL has `every x in <list> satisfies …` and
`some x in <list> satisfies …`, but feelc **rejects** them on purpose: the list can be runtime-sized,
which means an unbounded input space — fatal to the static verification that is feelc's whole point.
This was the #1 ranked philosophy-compatible gap in [comparison.md](../comparison.md).

The insight: the *quantifier* is fine; the *unbounded list* is the problem. A quantifier over a
**fixed, compile-time tuple of named scalars** keeps the input space hyper-rectangular, so verification
stays sound.

## Decision

Add a distinct, bounded surface — `every of {a, b, c} satisfies <body>` and
`some of {a, b, c} satisfies <body>` — where `{a, b, c}` is a literal tuple of scalar inputs/columns
and `?` is the element placeholder.

- Implemented as a **DSL-level macro** (`internal/dsl/dsl.go`, `expandBoundedQuantifier`) that rewrites
  the sugar to a plain FEEL boolean chain **before** parsing:
  `every of {a,b,c} satisfies ? < 26` → `((a < 26) and (b < 26) and (c < 26))`; `some` → `or`. The
  vendored FEEL parser is untouched (cf. ADR 0001/0020/0022). `?` is substituted outside string
  literals; the empty tuple is vacuously `true` (every) / `false` (some). The original text is kept as
  the decision's `Src` for traceability; the expansion drives the AST.
- A `decision name : type = … {…}` line (assignment before the brace) is now correctly dispatched as a
  literal expression, not a table header (`isTableHeader`).

## Verification soundness

The expansion is **plain `and`/`or` of the body over a finite, fixed set** — no new opcode, no list
type, no runtime-sized iteration. The geometric and SMT verifiers already handle `and`/`or`/comparisons,
so a bounded quantifier is analyzable for free and the input space stays hyper-rectangular. Soundness is
inherited, not re-argued.

## Consequences

- Closes the #1 philosophy-compatible gap; "for all of these / for some of these" rules are now
  expressible **without** a list type or unbounded iteration.
- The line stays bright: native `every x in <list> satisfies …` / `for … in …` (list possibly
  runtime-sized) remain **rejected** (guardian-of-scope tripwire `some i in [1,2] satisfies i > 0`
  stays red). Only the explicit `of {fixed tuple}` form is admitted.
- v1 scope: the sugar is the **whole** RHS of a literal-expression decision (not yet a nested
  sub-expression). Compose via an intermediate decision if needed.
- Tests: `internal/engine/feel_ext_test.go` (`TestBoundedQuantifiers`, incl. the native-form rejection);
  WASM parity in `packages/engine/test/conformance.test.ts`.

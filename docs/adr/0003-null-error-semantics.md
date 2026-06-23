# ADR 0003 — Semantics of null and errors (trivalence)

- **Status**: accepted (2026-06-22)
- **Deciders**: maxgfr

## Context

FEEL is **trivalent**: `null` is a first-class value and propagates without raising
an exception. This is precisely what makes ~30% of the DMN TCK fail on naive implementations
(the adversarial review flagged it). We must therefore **freeze an explicit null/error decision
table** and test it, *before* targeting the TCK. feelc distinguishes three worlds: the input
boundary, table cells, and expressions.

## Decision (policy v2)

### 1. Input boundary
- A **missing** external input referenced by a decision → **error** (`unknown variable at
  runtime`). This is a contract violation by the caller, not a FEEL `null`. Fail-fast.
- An input explicitly `null` (JSON `null`) → FEEL `null` value that follows the rules below.

### 2. Table cells (unary tests)
- A cell tested against a `null` value (`< 580`, `[a..b)`, `= x`, set) → **does not match**
  (`false`), **without error**. `null` satisfies no condition. → the `default` row (if present)
  takes over; otherwise the decision is `null`.
- A **non-null inconsistent type** in a cell (e.g. comparing a `string` to a numeric threshold when
  the typecheck should have forbidden it) → **error** (a real anomaly, not a business case).

### 3. Decisions
- Table with no winning rule **and no `default`** → **`null`** result (and the **completeness
  check (Slice 4) will flag the gap** with a counter-example — nothing is silently hidden).
- Expression: **arithmetic with a `null` operand** → propagates **`null`** (never an exception).
- **Division by zero** → **error** (undefined case, distinct from null propagation; choice driven by
  auditability: a division by zero is a defect in the model/data, not a business result).

## Deferred (assumed, never silently conformed)

- **Full trivalent boolean logic** at the expression level (`null and false`, `null or true`,
  comparison `null < x` → `null` rather than `false`). In v2 a comparison on `null` within an
  **expression** yields `false` (conservative). Fine-grained boolean trivalence will arrive with the
  DMN TCK harness (cf. plan), accompanied by dedicated tests. No claim of TCK conformance until this
  is implemented.

## Consequences

- **Deterministic and tested** behavior on the common null cases of the 4 examples.
- Cells tolerate `null` (robustness: an upstream decision that returns `null` does not blow up the
  downstream, it falls onto the `default`).
- The boundary (missing input, division by zero) **fails outright** rather than producing a
  misleading result — consistent with the auditability objective.

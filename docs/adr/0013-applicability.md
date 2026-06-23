# ADR 0013 — Applicability (non-applicable values)

- **Status**: accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

Eligibility/entitlement rules need a distinct *non-applicable* state: a rule that does
not apply yields a value that propagates specially (in a sum, non-applicable
terms count as 0; in a product they poison the result). feelc had only null (ADR 0003), which
*propagates* through arithmetic (poisoning sums) — wrong for "this benefit simply doesn't apply".

## Decision

Add a distinct **non-applicable** value, `ir.TagNA` (≠ null), and gate decisions with:

```
decision housing_aid : number {
  = 200
  applicable if income < 1500       # or: not applicable if <cond>
}
```

- **Lowered, not interpreted.** `applicable if C` compiles to `if C then E else NA` (and the negation
  to `if C then NA else E`), reusing the existing `emitIf`; the NA branch is an internal sentinel that
  the lowerer turns into a non-applicable **constant**. So the VM gains no gate logic and the codec
  needs no change — `TagNA` encodes tag-only like null (no version bump, existing hashes unchanged).
- **Propagation** (`internal/vm/expr.go`): in `+`/`-` a non-applicable operand acts as **0** (it drops
  out of a sum; a sum of only non-applicable terms is non-applicable); in `*`/`/` and `floor/ceiling/
  round` it **poisons** to non-applicable. In matching it behaves like null (satisfies no cell). It
  renders as the JSON string `"non-applicable"` (`ir.NotApplicable`), distinct from null everywhere.
- **Scope (v1):** `applicable if` is supported on **expression** decisions (`decision x : T { = <expr>
  applicable if … }`); on a table it is a loud compile error (use an expression or a `default`).
  Reaching a non-applicable value in a comparison/boolean/`if`-condition is a loud runtime error
  (honest — never silently conform).

## Consequences

- Faithful eligibility modelling (sum-of-optional-benefits) without null-poisoning; the deterministic
  core, codec and hash are unchanged (NA is tag-only and the gate is pure lowering).
- Relationship to ADR 0003: null = "unknown/missing" (propagates); non-applicable = "rule does not
  apply" (drops from sums). Two distinct three-valued concerns, kept separate.

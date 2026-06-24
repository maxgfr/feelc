# ADR 0023 — String predicates: `starts_with` / `ends_with` / `contains`

- **Status**: Accepted (2026-06-24)
- **Deciders**: maxgfr

## Context

Real routing/eligibility rules constantly branch on the *shape* of a code or identifier: an IBAN
country prefix, a policy-number family, a SKU containing a marker, a postal-code band. feelc had no
way to express "this string starts with / ends with / contains that one", so such rules either could
not be modelled or had to be pushed upstream. This was the #2 ranked philosophy-compatible gap in
[comparison.md](../comparison.md).

The risk to avoid: turning feelc into a **string-manipulation library** (`substring`, `upper case`,
`replace`, regex). Those are unbounded / open-ended and stay out of scope. What is safe is a small,
closed set of **predicates** — pure, total, deterministic `(string, string) → boolean` functions.

## Decision

Add three string **predicates** (not transformations): `starts_with(s, t)`, `ends_with(s, t)`,
`contains(s, t)`, each returning a boolean.

- Function-form built-ins in the expression lowerer (`internal/compiler/lower_expr.go`), new opcodes
  `OpStartsWith` / `OpEndsWith` / `OpContains`, executed in `internal/vm/expr.go` via
  `strings.HasPrefix` / `HasSuffix` / `Contains` (pure, total, deterministic). Operands must both be
  strings (`null` / non-applicable propagate; a non-string operand is a loud error). Handled in all
  four opcode walkers (VM, SMT, `maxStack`, units — string operands are dimensionless, result
  dimensionless boolean).
- Usable as a **literal-expression decision** (`decision d : boolean = starts_with(code, "EU")`) or
  inside a table cell as `starts_with(?, "EU")`.

## Verification soundness

A string predicate is **opaque** to the geometric / SMT analyzers (strings are not an ordered numeric
domain). This is safe because a cell that uses one is non-geometric (`Op=Prog`), and the verifier
already routes any `Op=Prog` cell to the SMT backend or, failing that, to an honest **not-verifiable**
degradation — it **never** claims completeness or conflict-freedom for such a table. Regression-locked
by `TestStringPredicateCellDegradesNotVerifiable`. As a literal-expression decision there is no table
obligation at all. So the addition cannot make the verifier unsound.

## Consequences

- Closes the #2 philosophy-compatible gap; code/policy routing is now first-class.
- The line stays bright: predicates only. `substring`, `upper case`, `replace`, regex, and every other
  string-manipulation function remain rejected (guardians of scope).
- Tests: `internal/engine/feel_ext_test.go` (`TestStringPredicates`,
  `TestStringPredicateCellDegradesNotVerifiable`). The `starts_with` / `contains` conformance
  tripwires move from *rejected* to *supported*.

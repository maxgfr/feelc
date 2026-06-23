# ADR 0011 — Progressive brackets (`bracket:`), lowered to arithmetic

- **Status**: accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

Tax/benefit rules constantly need **marginal-rate brackets** (common in fiscal and social law). Expressing them as a decision table or hand-written `if/then/else` is verbose and
error-prone. We want a concise, first-class `bracket` mechanism — without growing the deterministic
VM.

## Decision

A `bracket:` directive inside a decision block declares a marginal-rate schedule over one numeric
input:

```
decision tax : number {
  bracket: taxable
  [0..11294)      => 0%
  [11294..28797)  => 11%
  >= 82341        => 41%
}
```

It compiles to **ordinary arithmetic bytecode** — `tax = Σ tranches (clamp(x, lo, hi) - lo) × rate`,
built as a FEEL AST of `if/then/else` + `+ - *` and run through the existing `lowerExpr` — so there is
**no new VM opcode** and the result is exact and deterministic. The decision becomes a literal-expression
in the IR. Conditions reuse the normal cell normalization (`[lo..hi)` and `>= lo`); a `default` row is
rejected. Rates accept **percent literals** (`30%` ≡ the exact decimal `0.30`), a general whole-cell
feature added to the parser (whole-cell `%`).

## Consequences

- New concise authoring construct; zero VM/determinism impact (pure lowering).
- Because it lowers to an expression, `verify` treats it as evaluated (not geometrically proven);
  correctness is covered by unit tests + a worked example (`examples/income-tax/`).
- Percent literals are whole-cell only (`30%`), not inside comparisons (`>= 30%`), keeping the parser
  change tiny and unambiguous.

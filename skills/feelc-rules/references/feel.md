# DSL / FEEL subset supported by feelc (v2)

> The typecheck is the gatekeeper: any construct outside this scope **fails
> compilation** with a clear message. This is intentional (reject rather than accept-then-misinterpret).

## Structure of a `.rules` file

```
model "name" {
  rounding: half_even          # optional
}

input <name> : <type> [<domain>]
type <Name> = context { f1: t1, f2: t2 }     # multi-field output type (optional)

# Decision = literal expression (scalar type):
decision <name> : <type> = <FEEL expression>

# Decision = table:
decision <name> : <type|ContextType> {
  needs: a, b, c                 # input columns (inputs OR upstream decisions -> DRG)
  hit: <policy>
  priority: v1, v2, ...          # ONLY if hit: priority (from most to least prioritized)
  cell | cell | ... => output | output | ...
  default  | ...        => output | ...        # optional: otherwise non-match = null
}
```

## Types

`number` (**exact** decimal, never floating-point), `string`, `boolean`, `date` and `duration`
(whole-day, ISO — see *Temporal* below), and the declared `context { … }` types (multi-column output,
**output only** — a `context` type cannot be used as an `input`). A decision `: number|string|boolean|date|duration`
has **one** scalar output; a decision `: MyContext` has one output per context field (as many output columns).

## Input domains (used for completeness checking)

`in [a..b]` · `[a..b)` · `(a..b]` · `(a..b)` · `>= x` · `> x` · `<= x` · `< x` · `in {v1, v2, …}`

## Condition cells (unary tests)

| Form | Meaning |
|---|---|
| `-` | anything (don't care) |
| `580`, `"gold"`, `true` | equality to the literal |
| `< x` `<= x` `> x` `>= x` `!= x` | comparison (x = literal, **or another column/variable**) |
| `[a..b)` etc. | interval (open/closed bounds) |
| `"a","b"` or `1,2,3` | set = OR (membership) |
| `not(literal)` | negation of a literal |

⚠️ `not(< 18)` (negation of a comparison) is **not** supported.

## Table outputs

**Literals only** (`true`, `"text"`, `42`). A computed output is done via a separate
**literal-expression decision** (`decision x : number = …`), not in an output cell.

## Expressions (in `decision … = <expr>` and `?`-vs-column cells)

Supported: literals, variables (input or upstream decision), `+ - * /`, comparisons
`< <= > >= = !=`, `and`, `or`, `not(x)`, parentheses, the conditional `if c then a else b`, the
**single-arg** built-ins `floor(x)` / `ceiling(x)` / `round(x)` / `abs(x)` / `trunc(x)` (HALF_EVEN), the
**two-arg** built-ins `round(x, n)` (n decimal places), `modulo(x, y)` (floored, DMN; modulo-by-zero
errors) and `power(x, n)` (integer-exponent exponentiation, exact; non-integer/negative `n` errors),
the **string predicates** `starts_with(s, t)` / `ends_with(s, t)` / `contains(s, t)` → boolean (code/
policy routing; not a string library), **bounded quantifiers** `every of {a, b, c} satisfies ?` /
`some of {a, b, c} satisfies ?` over a fixed scalar tuple (`?` = element), and **BKM** invocation
(`name(a, b)`, inlined at compile time). Example:
`if annual_income > 0 then round(monthly_debt / (annual_income / 12), 2) else 0`.

**NOT supported** (fails compilation): **multi-argument** built-ins beyond `round(x, n)` / `modulo(x, y)` / `power(x, n)`
(`substring(s, i, n)`, other string/list functions…); native unbounded `for` / `some x in <list>` /
`every x in <list>`, lists/filters/higher-order functions (the **bounded** `every/some of {…} satisfies ?`
form IS supported); the `**` / `^` operators (use `power(x, n)`), unary minus; times of day, date-times, year-month durations,
timezones; `?` inside a literal-expression (reserved for cells). ⚠️ `sum`/`min`/`max`/`count` are
**COLLECT hit-policy aggregations** (see below), not functions.

## Temporal (date & duration)

Whole-day model (a `date` and a `duration` are integer day counts):
- **literals**: `date("YYYY-MM-DD")`, `duration("P30D")` (ISO-8601, day granularity);
- **arithmetic**: `date − date = duration`, `date ± duration = date`, `duration ± duration = duration`;
- **comparisons**: `= != < <= > >=` between two dates or two durations.

Out of scope (fails): times of day, date-times, year-month durations, timezones, mixing a date with a
bare number. The engine never reads the clock — pass "today" as an input.

## Units (money & dimensions)

A numeric input/decision may carry a unit: `input salary : number >= 0 unit "EUR/month"`. Units are
checked at compile time (dimensional analysis: `EUR + EUR/month` is rejected); runtime values stay plain
decimals. Money = `number unit "<currency>"`.

## Progressive brackets (marginal-rate schedules)

```
decision tax : number {
  bracket: taxable
  [0..10000)     => 0%
  [10000..30000) => 11%
  >= 30000       => 30%
}
```
Computes `Σ (clamp(x,lo,hi) − lo) × rate`, lowered to arithmetic (no `default`). Rates accept `%` literals.

## Applicability (eligibility gating)

A block-form expression decision can be gated:
`decision aid : number { = 200  applicable if income < 1500 }` (or `not applicable if <cond>`). A
non-applicable result drops out of sums / poisons products and renders as `"non-applicable"`.

## BKM (reusable pure functions)

`bkm dti(d:number, i:number):number = d / (i / 12)`, then call `dti(monthly_debt, annual_income)` in any
expression. Inlined at compile time; self/mutual recursion is rejected.

## Hit policies

| `hit:` | Semantics |
|---|---|
| `unique` | at most 1 rule matches; ≥2 → **error** |
| `any` | several may match but with **same outputs**; divergent outputs → **error** |
| `first` | the 1st matching rule wins (order = priority) |
| `priority` | among the matching rules, the highest-priority output (`priority:` line) |
| `collect` | list of all matching outputs |
| `collect sum` / `min` / `max` / `count` | numeric aggregation of matching outputs |
| `rule order` | list of outputs, in rule order |
| `output order` | list of outputs, ordered by output-value priority (`priority:` line) |

## `null` values and errors (deterministic, frozen)

- Cell tested against `null` → **does not match** (falls to `default`, otherwise decision = `null`).
- Arithmetic with a `null` operand → result **`null`** (propagation).
- **Division by zero** → **error**.
- Required input **missing** → **error**.

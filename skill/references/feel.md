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

`number` (**exact** decimal, never floating-point), `string`, `boolean`, and the declared
`context { … }` types (multi-column output). A decision `: number|string|boolean` has **one** scalar output;
a decision `: MyContext` has one output per context field (as many output columns).

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
`< <= > >= = !=`, `and`, `or`, parentheses. Example: `monthly_debt / (annual_income / 12)`.

**NOT supported in v2** (fails compilation): function calls (`sum(...)`, `floor(...)`…),
`if/then/else`, `not(...)` as an expression, `**`, lists/ranges as expressions, unary minus,
dates/durations/timezones. ⚠️ `sum`/`min`/`max`/`count` do **not** exist as FEEL functions — they
are **COLLECT hit policy aggregations** (see below).

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

## `null` values and errors (deterministic, frozen)

- Cell tested against `null` → **does not match** (falls to `default`, otherwise decision = `null`).
- Arithmetic with a `null` operand → result **`null`** (propagation).
- **Division by zero** → **error**.
- Required input **missing** → **error**.

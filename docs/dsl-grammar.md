# `.rules` DSL Grammar

The feelc source language (the **source of truth**) is deliberately minimal and **line-oriented**.
Parser: `internal/dsl`. Any construct outside the subset **fails outright** (reject rather than
accept-then-misinterpret).

## File Structure

```
model "<name>" {}

input <name> : <type> [<domain>]
...

type <Name> = context { <field>: <type>, ... }      # optional
bkm <name>(<p>:<type>, ...): <type> = <FEEL expr>    # optional

decision <name> : <type> = <FEEL expr>               # literal-expression decision
decision <name> : <type> {                            # decision table
  needs: <a>, <b>, ...
  hit: <policy>
  priority: <v1>, <v2>, ...                          # only if hit: priority
  <cond> | <cond> => <output> | <output>
  default        => <output> | <output>              # optional
}
```

- `# ...`: comment (outside strings), stripped on read (**not preserved** by `feelc fmt`).
- `<type>`: `number`, `string`, `boolean`, or a declared `type ... = context {...}` name.

## Declarations

| Form | Meaning |
|-------|------|
| `model "credit" {}` | model name (the `{ rounding: ... }` body is ignored, not stored) |
| `input credit_score : number in [300..850]` | input data + **domain** (completeness check) |
| `input hire_date : date` / `input notice : duration` | temporal types (whole-day, ADR 0014); literals `date("YYYY-MM-DD")`, `duration("P30D")` |
| `type Out = context { ok: boolean, label: string }` | multi-column output type |
| `bkm dti(d:number, i:number):number = d / (i / 12)` | parameterized pure function, **inlined** at compile time |

### Input Domains (optional)

`in [a..b]` / `in (a..b)` (open bounds), `>= 0`, `> 0`, `<= 100`, `< 100`, `in { "a", "b" }`
(enumeration). An unrecognized form is ignored (no domain).

A numeric input may carry an optional **unit**: `input salary : number >= 0 unit "EUR/month"`. Units
are checked at compile time (dimensional analysis: `EUR + EUR/month` is rejected) and are a type-level
concern only — runtime values stay plain decimals. Money = `number unit "<currency>"`. See
[ADR 0012](adr/0012-units.md).

### Annotations (optional)

Documentation/traceability lines placed **immediately before** an `input` or `decision`:

```
@title    "Credit eligibility"
@doc      "Whether the applicant qualifies, and why."
@question "What is your annual income?"      # prompt for the simulator (inputs)
@source   "Lending policy v3, section 2"     # traceability to a law article / spec
```

They are **descriptive only** — they do not affect evaluation or the model hash (and are dropped on
`.ir.bin` serialization). They drive `explain`, `graph` labels, the `serve --ui` simulator and
generated docs. A dangling annotation (not followed by an input/decision) is an error.

## Decisions

- **literal-expression**: `decision x : number = <expr>` — a FEEL expression (see
  [feel-subset.md](feel-subset.md)). `?` (column value) is **forbidden** here (reserved for cells).
- **table**: `needs:` (input columns), `hit:` (policy), rules, optional `default`.
- **bracket** (progressive/marginal schedule): `bracket: <number input>` then tranches
  `[lo..hi) => <rate>` and a top `>= lo => <rate>`. Computes `Σ (clamp(x,lo,hi)-lo) × rate`, lowered to
  arithmetic (no `default`). Rates accept percent literals. See [ADR 0011](adr/0011-progressive-brackets.md).
- **applicability**: an expression decision in block form may be gated:
  `decision aid : number { = 200  applicable if income < 1500 }` (or `not applicable if <cond>`). A
  non-applicable result drops out of sums and poisons products; it renders as `"non-applicable"`. See
  [ADR 0013](adr/0013-applicability.md).

### Hit policies

`first`, `unique`, `any`, `priority` (+ `priority:` line), `rule order`,
`collect` / `collect sum` / `collect min` / `collect max` / `collect count`.

### Cells

A **condition** cell is a FEEL *unary test*: `-` (any), literal (`580`, `"urban"`,
`true`), comparison (`< 580`, `>= 18`), interval (`[580..680)`), set (`"a", "b"`),
negation (`not(<test>)`), or a free expression referencing `?`/other columns (compiled to
bytecode, called *Op=Prog*, non-geometric). An **output** cell is a literal. A whole-cell
**percent literal** (`30%`) is the exact decimal `0.30`.

## Errors

Every compilation error is a positioned **structured** diagnostic — see
[error-schema.md](error-schema.md) (`--json` → `{file,line,col,code,message,suggestion}`).

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
| `type Out = context { ok: boolean, label: string }` | multi-column output type |
| `bkm dti(d:number, i:number):number = d / (i / 12)` | parameterized pure function, **inlined** at compile time |

### Input Domains (optional)

`in [a..b]` / `in (a..b)` (open bounds), `>= 0`, `> 0`, `<= 100`, `< 100`, `in { "a", "b" }`
(enumeration). An unrecognized form is ignored (no domain).

## Decisions

- **literal-expression**: `decision x : number = <expr>` — a FEEL expression (see
  [feel-subset.md](feel-subset.md)). `?` (column value) is **forbidden** here (reserved for cells).
- **table**: `needs:` (input columns), `hit:` (policy), rules, optional `default`.

### Hit policies

`first`, `unique`, `any`, `priority` (+ `priority:` line), `rule order`,
`collect` / `collect sum` / `collect min` / `collect max` / `collect count`.

### Cells

A **condition** cell is a FEEL *unary test*: `-` (any), literal (`580`, `"urban"`,
`true`), comparison (`< 580`, `>= 18`), interval (`[580..680)`), set (`"a", "b"`),
negation (`not(<test>)`), or a free expression referencing `?`/other columns (compiled to
bytecode, called *Op=Prog*, non-geometric). An **output** cell is a literal.

## Errors

Every compilation error is a positioned **structured** diagnostic — see
[error-schema.md](error-schema.md) (`--json` → `{file,line,col,code,message,suggestion}`).

# feelc structured error schema

**Compilation** errors (parsing `.rules` + typecheck/lowering) are structured,
positioned objects, serializable to JSON via `--json`. This is the contract that the
authoring skill reads for its red→green loop (fixing the source from
`line`/`col`/`suggestion`).

## Text format (always)

`Error()` remains **backward-compatible**:

- without a known file: `ligne <N>: <message>`
- with a file        : `<file>:<line>[:<col>]: <message>`
- global error (without position): `<message>`

The `suggestion` **never** appears in the text (only in JSON / a dedicated human
rendering) so as not to break assertions on substrings.

## JSON format (`--json`)

Emitted on **stdout** by `feelc run|verify|check --json` when compilation fails:

```json
{
  "file": "credit.rules",
  "line": 12,
  "col": 7,
  "code": "DSL002",
  "message": "cellule FEEL invalide \"1 +\": ...",
  "suggestion": "..."
}
```

| Field        | Type   | Presence                                  |
|--------------|--------|-------------------------------------------|
| `file`       | string | omitted if unknown                        |
| `line`       | int    | always (0 if position unknown)            |
| `col`        | int    | omitted if 0 (unknown); 1-based           |
| `code`       | string | omitted if empty; **stable** (see below)  |
| `message`    | string | always; FR text identical to the text     |
| `suggestion` | string | omitted if empty                          |

`col` is computed **at the DSL split** (offset of the cell segment within the source line).
It is reliable for table cells (conditions/outputs); for literal-expression
expressions and single-line declarations, only `line` is guaranteed.

## Code catalog (STABLE — do not renumber)

Consumed by the skill: these codes are a contract. Sources: `internal/diag/diag.go`.

### `DSL*` — `.rules` parser

| Code   | Meaning                                           |
|--------|---------------------------------------------------|
| DSL001 | unrecognized instruction                          |
| DSL002 | invalid FEEL cell / expression (wraps the FEEL cause) |
| DSL003 | model without a `model "..."` declaration         |
| DSL004 | malformed `model` header                          |
| DSL005 | malformed `input`                                 |
| DSL006 | malformed decision header                         |
| DSL007 | unrecognized decision body line                   |
| DSL008 | malformed rule (`=>` missing)                     |
| DSL009 | empty cell                                        |
| DSL010 | malformed `type` declaration                      |
| DSL011 | unsupported type                                  |
| DSL012 | content after `{` on the header line              |

### `CMP*` — compiler / typecheck

| Code   | Meaning                                           |
|--------|---------------------------------------------------|
| CMP001 | reference to an undeclared name (`needs`/var)     |
| CMP002 | unsupported hit policy                            |
| CMP003 | unknown decision type                             |
| CMP004 | wrong number of conditions / outputs              |
| CMP005 | PRIORITY constraint not satisfied                 |
| CMP006 | COLLECT constraint not satisfied                  |
| CMP007 | construct outside the v2 subset                   |
| CMP008 | literal expected                                  |

## Scope

This schema covers **compilation errors**. The reports from `verify` (`Finding`) and
`check` (`Verdict`) have their own JSON form (aligned with the same style), unchanged here.
The raw FEEL cause remains wrapped (`Unwrap()` / `errors.As`), never rewritten.

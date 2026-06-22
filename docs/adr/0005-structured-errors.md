# ADR 0005 — Structured and positioned compilation errors

- **Status**: accepted (2026-06-22)
- **Deciders**: maxgfr

## Context

Compilation errors (`dsl.Parse` + `compiler.Compile`) were flat `fmt.Errorf`
in the format `"ligne %d: <message>"`. The authoring skill needs, for its red→green loop, a
**machine-exploitable** diagnostic: file, line, **column**, stable code and fix suggestion. Moreover `model.Cell.Col` was declared but **never populated** (always 0).

## Decision

Introduce `internal/diag.Error{File, Line, Col, Code, Message, Suggestion, Cause}` which:

1. **Stays backward-compatible** in text: `Error()` renders exactly `"ligne N: <message>"` when
   no file is known (existing tests match FR substrings — anti-regression safety net). With a file: `"file:line[:col]: message"`. The **suggestion is never**
   in `Error()`.
2. Exposes `MarshalJSON` → `{file,line,col,code,message,suggestion}` (omitempty), rendered on stdout
   by `feelc run|verify|check --json`.
3. Preserves the historical `%w` chaining via `Cause` + `Unwrap()` (raw FEEL errors stay
   wrapped, never rewritten).

**Positions.** `model.Cell.Col` is now filled, **computed at the DSL split** (cumulative offset of the
cell segment in the source line). Pitfall avoided: `feel.Node.TextRange().Column` is relative
to the isolated cell (each cell is parsed alone) — unusable as a line column. The
propagation of the **file name** goes through `dsl.ParseFile(path, src)` / `loader.CompileFile(path, src)`;
`Parse`/`Compile` remain `file=""` wrappers (compat). `engine.Run` remains `file=""`.

**Codes.** Stable `DSL*` / `CMP*` catalog frozen in `docs/error-schema.md` — contract consumed by
the skill, not renumbered.

## Consequences

- The skill can locate and fix an error without parsing FR text.
- Scope kept: only **compilation errors** are structured; the `verify`/`check` reports
  keep their form (aligned with the same JSON style), no scope-creep.
- `col` is only reliable for table cells; for single-line statements, `line` is enough
  (we do not promise a wrong column — honesty).

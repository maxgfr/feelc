# Supported FEEL subset

feelc reuses the `pbinitiative/feel` parser (forked and vendored under `third_party/feel`, cf.
[ADR 0001](adr/0001-feel-frontend.md) and [ADR 0004 §1](adr/0004-deferrals.md)) but **does not run**
its evaluator: feelc compiles to its own deterministic bytecode (exact decimals, cf.
[ADR 0002](adr/0002-decimal.md)). The scope is deliberately bounded; **everything else fails
loudly** (the compiler is the guardian of the scope).

## Expressions (literal-expression and Op=Prog cells)

Supported (`internal/compiler/lower_expr.go`):

- **literals**: numbers (exact decimals), strings, booleans;
- **variables**: input / upstream decision names; `?` = value of the current column
  (table cells only);
- **arithmetic**: `+ - * /` (exact decimal, division by zero = error);
- **comparisons**: `= != < <= > >=`;
- **logic**: `and`, `or`, `not(x)`;
- **conditional**: `if c then a else b` (compiled into `OpJmpFalse`/`OpJmp` jumps);
- **pure single-arg built-ins**: `floor(x)`, `ceiling(x)`, `round(x)`, `abs(x)`, `trunc(x)` (toward zero) — HALF_EVEN, deterministic;
- **deterministic two-arg built-ins** ([ADR 0020](adr/0020-deterministic-extra-builtins.md)): `round(x, n)` (round to `n` decimal places, HALF_EVEN) and `modulo(x, y)` (floored modulo `x − y·floor(x/y)`, DMN semantics; modulo-by-zero errors);
- **BKM invocation**: `name(a, b)` — **inlined** at compile time (AST substitution, zero call
  frame; self/mutual recursion is detected and **rejected**).

## Table cells (unary tests)

`-` (any), literal (equality), `< x` / `<= x` / `> x` / `>= x`, interval `[a..b]` / `(a..b)` /
`[a..b)`, set `a, b, c`, negation `not(<test>)` (stays **geometric**, hence analyzable by
verification), and free expression (reference `?`/other columns → *Op=Prog*, non-geometric).

## Out of scope (loud failure)

- **multi-argument** built-ins beyond the whitelist: `substring(s, i, n)`, other string/list functions, etc. ([ADR 0004 §3](adr/0004-deferrals.md)); `round(x, n)` and `modulo(x, y)` ARE supported ([ADR 0020](adr/0020-deterministic-extra-builtins.md));
- `for` / `some` / `every`, lists/filters, higher-order functions, `function(...)`;
- **out-of-subset temporal** forms: `time`, `dateTime`, year-month durations (`date` and day-based
  `duration` ARE supported — see below);
- `**` (power), operators not listed;
- `?` inside a **literal-expression** (reserved for table cells);
- **named** arguments (kwargs) in a BKM invocation.

## Temporal (date & duration)

Supported since [ADR 0014](adr/0014-temporal-types.md), on a **whole-day** model (a date is an integer
count of days; a duration is an integer count of days):

- **literals / constructors**: `date("YYYY-MM-DD")`, `duration("P30D")` (ISO-8601, day granularity);
- **arithmetic**: `date − date = duration`, `date ± duration = date`, `duration ± duration = duration`;
- **comparisons**: `= != < <= > >=` between two dates or two durations;
- inputs typed `: date` / `: duration` (see `examples/employment`).

Everything else temporal fails loudly: times of day, date-times, and year-month durations are out of
scope (above), and mixing a date with a bare number is a type error.

## Determinism

**Frozen** decimal context (precision 34 / HALF_EVEN), no source of nondeterminism in the
decision path. Outputs are bit-for-bit replayable across platforms (CI goldens amd64+arm64).
Formal verification ([verify](../README.md)) proves completeness/conflicts/subsumption on the
geometric layer; Op=Prog cells are reported as `not-verifiable` (or routed to SMT
under `-tags smt`, [ADR 0007](adr/0007-smt-backend.md)).

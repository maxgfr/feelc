# ADR 0014 — Date & duration types (whole-day)

- **Status**: accepted (2026-06-23)
- **Deciders**: maxgfr

## Context

Calendar logic (dates and durations) is a defining need for real rules. feelc deferred temporal types
(ADR 0004). Real rules (leave, tenure, deadlines) need dates and durations with sound arithmetic.
Calendar arithmetic mixing years/months (variable length) with days is notoriously unsound, so the
challenge is to add dates **without** compromising the exact, deterministic core.

## Decision

Add `date` and `duration` as first-class types, modelled as **integer days** (whole-day granularity)
stored in the existing `Value.Num` decimal field, with distinct tags `TagDate`/`TagDuration`:

- A `date` is the integer count of days since the Unix epoch (UTC); a `duration` is a whole number of
  days (ISO `P<n>D`). Date arithmetic is therefore exact integer/decimal math — no calendar
  ambiguity, fully deterministic.
- **Operations**: `date − date = duration`, `date ± duration = date`, `duration ± duration =
  duration`, and ordered comparisons between two values of the **same** temporal type. Everything else
  (`date + date`, scaling a duration, `floor(date)`, comparing a date to a number, …) is a **loud
  error** — never a silent nonsense result.
- **Literals**: `date("YYYY-MM-DD")` and `duration("P30D")` are compile-time constants (parsed in the
  lowerer). Date/duration inputs are supplied as ISO strings and coerced by their declared type
  (`ir.CoerceInputs`). They render back as ISO strings (`ToAny`).
- **Codec**: `TagDate`/`TagDuration` carry their day-count in `Num`, encoded like `TagNumber`.
  Existing models contain neither tag, so encodings/hashes are unchanged — we deliberately keep
  `codecVersion = 1` (bumping it would change *every* model's hash for a purely additive tag).
  Trade-off: a `.ir.bin` that uses temporal (or applicability) tags is only readable by a feelc built
  with this version; an older binary would mis-read it. Acceptable because `.ir.bin` is a local build
  artifact, not a long-lived cross-version interchange format.
- **Verification**: a date/duration *table column* compared to a temporal literal is non-geometric, so
  it flows through the honest `Op=Prog` → `not-verifiable` path (the geometric witness machinery is
  untouched). Expressions evaluate deterministically.
- **DMN**: `xsd:date` → `date`, `xsd:dayTimeDuration`/`duration` → `duration` on import. The TCK
  parser accepts date/duration; types still out of scope (`time`, `dateTime`, `yearMonthDuration`,
  `function`) remain honest skips.

## Consequences

- Sound, deterministic, exact temporal arithmetic with zero impact on the existing core (dates reuse
  the decimal engine; the VM, hash and `.ir.bin` are unaffected for non-temporal models).
- **Out of scope (deliberate, to stay sound):** year/month durations (variable length), times of day,
  date-times, and duration scaling. These fail loudly; supersedes the ADR 0004 temporal deferral for
  the whole-day subset.

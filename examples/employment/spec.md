# Example — date & duration types

`date` and `duration` are first-class (ADR 0014). Dates are calendar days; durations are whole days
(ISO `P<n>D`). Supported: `date - date = duration`, `date ± duration = date`, `duration ± duration`,
and comparisons. Literals: `date("YYYY-MM-DD")`, `duration("P365D")`. Date/duration inputs are given
as ISO strings.

## Try it

```sh
feelc run --rules examples/employment/tenure.rules --decision tenure --input '{"hire_date":"2020-01-01","as_of":"2024-01-01"}'
# P1461D   (4 years incl. one leap day)
feelc run --rules examples/employment/tenure.rules --decision eligible --input '{"hire_date":"2024-01-01","as_of":"2024-06-23"}'
# false    (under one year)
```

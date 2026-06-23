# Example — applicability (non-applicable)

Each benefit is gated with `applicable if`. A benefit that does not apply produces a **non-applicable**
value (distinct from null/0). In a sum, non-applicable terms drop out; if all terms are
non-applicable, the total is itself non-applicable. See [ADR 0013](../../docs/adr/0013-applicability.md).

## Try it

```sh
feelc run --rules examples/social-aid/aid.rules --decision total_aid --input '{"income":900,"is_student":true}'
# 350  (200 + 150)
feelc run --rules examples/social-aid/aid.rules --decision total_aid --input '{"income":900,"is_student":false}'
# 200  (student grant non-applicable -> drops from the sum)
feelc run --rules examples/social-aid/aid.rules --decision total_aid --input '{"income":2000,"is_student":false}'
# non-applicable  (no benefit applies)
```

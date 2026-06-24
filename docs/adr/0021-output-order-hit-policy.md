# ADR 0021 — DMN OUTPUT ORDER hit policy + PRIORITY/OUTPUT ORDER import fidelity

- **Status**: Accepted (2026-06-24)
- **Deciders**: maxgfr

## Context

feelc supported 6 of the 7 DMN decision-table hit policies. The two remaining DMN-TCK gaps were both
about the **priority-ordered** policies:

1. **OUTPUT ORDER was unsupported** — DMN's seventh hit policy (2 DMN-TCK cases). It returns *all*
   matching rules' outputs, ordered by the **output-value priority** (the declared `<outputValues>`,
   decreasing), as opposed to RULE ORDER which returns them in *rule* order.
2. **PRIORITY import degraded to FIRST** (1 DMN-TCK case). `feelc import` mapped a DMN
   `hitPolicy="PRIORITY"` table to `hit: first` with a warning, because it never read the
   `<outputValues>` that define the priority ordering — so the imported model could compute a
   different result than the source DMN.

Both gaps were honest degradations (never *wrong* values on supported features), but they are
deterministic, total, and fully verification-compatible — they were excluded only by missing wiring,
not by feelc's philosophy. Closing them brings feelc to **7/7 DMN hit policies**.

## Decision

Add the **OUTPUT ORDER** hit policy and make DMN import of **PRIORITY / OUTPUT ORDER** faithful.

- **New hit policy `output order`** (`ir.HitOutputOrder`, appended to the `HitPolicy` enum so the
  canonical IR codec stays backward-compatible). Parsed from `hit: output order`
  (`internal/compiler/compiler.go`). Like PRIORITY it requires a single scalar output and a
  `priority:` line; like RULE ORDER / COLLECT it returns a **list**. The VM
  (`internal/vm/vm.go`) collects all matches and `sort.SliceStable`-orders them by `rank(t.Priority, …)`
  (the same helper PRIORITY uses), so equal-priority outputs keep a deterministic order. The ordering
  is factored into `orderByPriority`, shared by `Eval` and the trace/explain path so they never
  diverge.
- **Verification**: OUTPUT ORDER is a multi-hit (list) policy, so — exactly like RULE ORDER /
  COLLECT — it has no completeness or conflict obligation (a region covered by 0 rules yields an empty
  list, overlaps are intended). The geometric and SMT layers already treat unknown multi-hit policies
  as "no gap / no conflict", so they stay **sound** with no new proof obligation. The priority-coverage
  hygiene check (a rule output absent from the `priority:` list) is extended to OUTPUT ORDER.
- **Import fidelity** (`internal/dmnxml/import.go`): `<outputValues>` is now read off the ranked
  output and emitted as a DSL `priority:` line; DMN `PRIORITY → hit: priority` and
  `OUTPUT ORDER → hit: output order`, each with that priority line. Export
  (`internal/dmnxml/export.go`) maps `output order → OUTPUT ORDER`.

## Consequences

- **7/7 DMN hit policies**; the 3 remaining DMN-TCK Level-2 failures (2 OUTPUT ORDER + 1 PRIORITY
  import) are closed. Decision-table conformance is no longer gated on these.
- No change to determinism, exact decimals, or verification soundness — OUTPUT ORDER is a pure,
  total, order-by-priority projection of the matched set.
- Tests: `internal/engine/hitpolicy_test.go` (`TestHitOutputOrder`), `internal/dmnxml/import_test.go`
  (`TestImportPriorityFidelity`, `TestImportOutputOrderFidelity`). The two "OUTPUT ORDER is deferred"
  guardian tests were flipped to point at a still-unsupported policy (`collect avg`).
- `priority:` lines are now meaningful for two policies; the compiler requires one for both.

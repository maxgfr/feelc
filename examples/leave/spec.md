# Example — metadata & law traceability

A tiny model showing the documentation/traceability annotations (`@title`, `@doc`, `@question`,
`@source`). They are **descriptive only** — they never change evaluation or the model hash — and
answer Catala's "tie a rule to the legal article it encodes" while driving `explain`, the graph
labels, the simulator form (`@question`) and generated docs.

## Decision

- **`eligible`** (`boolean`, hit `first`): true once `months_employed >= 12`.

## Try it

```sh
# The justification trace carries @title and @source (auditability):
feelc explain --rules examples/leave/leave.rules --decision eligible --input '{"months_employed":18,"weekly_hours":35}' --json
#   { "decision": "eligible", "title": "Parental leave eligibility",
#     "source": "Labor Code, Art. L1225-35", ... "output": true }

# The graph labels the nodes; the model introspection exposes the metadata:
feelc graph --rules examples/leave/leave.rules --format mermaid
```

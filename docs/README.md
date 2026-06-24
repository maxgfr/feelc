# feelc documentation

**feelc** is an AI-native DMN/FEEL business-rules engine compiled to Go: an LLM authors the rules, and a
deterministic engine compiles, verifies and runs them — no LLM in the core. This is the documentation map.

The docs come in three layers:

- **Pitch & commands** — the [root README](../README.md) (what feelc is, headline commands, a sample rule).
- **Reference** (this folder) — *how* each surface works.
- **Rationale** — the [Decisions](decisions.md) summary explains *why* it is built this way.

## Reference

| Page | What it covers |
|------|----------------|
| [DSL grammar](dsl-grammar.md) | The `.rules` language: model, inputs, decisions, tables, hit policies, brackets, units, applicability. |
| [FEEL subset](feel-subset.md) | The supported expression/cell subset (and what fails loudly), including temporal types. |
| [CLI reference](cli.md) | Every `feelc` subcommand, its flags, and exit codes. |
| [HTTP API](http-api.md) | The complete `feelc serve` route table (single-model + project + AI + health). |
| [Project mode](project-mode.md) | Multi-module workspaces: namespaced merge, `uses` bindings, the web editor. |
| [AI authoring](ai-authoring.md) | Bring-your-own-LLM authoring: the chat UI, the portable skill, and the red→green ingest loop. |
| [IR format](ir-format.md) | The canonical compiled-model wire format (`.ir.bin`) and its hash. |
| [Error schema](error-schema.md) | The structured `diag.Error` shape returned by the compiler/API. |
| [Architecture](architecture.md) | Package map and the compile pipeline (for contributors). |
| [Decisions](decisions.md) | The key engineering decisions, one line each (links to the full ADRs). |

## Quickstart

```sh
go build -o feelc ./cmd/feelc                      # or grab a release binary

cat > credit.rules <<'EOF'
model "credit" { rounding: half_even }
input score : number in [300..850]
decision eligible : boolean {
  needs: score
  hit: first
  >= 680 => true
  default => false
}
EOF

feelc verify --rules credit.rules                  # prove completeness / no conflicts
feelc run    --rules credit.rules --decision eligible --input '{"score":720}'   # → true
feelc serve  --ui                                  # author by chatting with your own LLM at :8080
```

Prefer a browser? The [playground](../playground/) runs the real engine compiled to WebAssembly — no
backend, no install. Want to contribute? See [CONTRIBUTING](../CONTRIBUTING.md).

# Providing the `feelc` binary

The wrapper `scripts/feelc-skill.mjs` looks for `feelc` in this order:

1. **`$FEELC_BIN`** — explicit path to a feelc binary.
   ```sh
   export FEELC_BIN=/path/to/feelc
   ```
2. **`feelc` on the PATH** — if you installed it globally.
3. **Binary at the repository root** — `../../../feelc` (the skill lives in `feelc/skills/feelc-rules/`).
4. **Automatic build** — if the skill runs inside the repository (`../../../go.mod` present) and `go`
   is installed, the wrapper runs `go build -o ../../../feelc ./cmd/feelc`.

> The skill is integrated into the `feelc` repository (under `skills/feelc-rules/`). When used INSIDE the repository, 3/4 are sufficient.
> For a standalone installation (copy of the skill alone), prefer 1 (`$FEELC_BIN`) or 2 (PATH).

## Getting feelc

- Clone and build (Go ≥ 1.22):
  ```sh
  git clone https://github.com/maxgfr/feelc
  cd feelc && go build -o feelc ./cmd/feelc
  export FEELC_BIN="$PWD/feelc"
  ```
- Verify: `node skills/feelc-rules/scripts/feelc-skill.mjs version` → `feelc <version>`.

The wrapper then relays all subcommands as-is: `verify`, `run`, `serve`, `version`.

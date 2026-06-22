# ADR 0006 — Canonical IR serialization + compiled model hash

- **Status**: accepted (2026-06-22)
- **Deciders**: maxgfr

## Context

feelc sells **bit-for-bit cross-platform determinism**. It was missing (1) a distribution
format for the **already-compiled** model (`.ir.bin`) to execute without re-parsing/re-compiling,
and (2) a **canonical identity** of the compiled model to freeze non-regression goldens
(Slice 19). The service hash was until now `sha256(source)`: sensitive to text formatting,
not to semantics.

## Decision

**Manual, length-prefixed big-endian** encoding in `internal/ir/codec.go`:

- `Encode(cm) ([]byte, error)` / `Decode([]byte) (*CompiledModel, error)` / `Hash(cm) ([32]byte, error)`.
- Header `magic ("FLIR") + version (uint16)`. `IsEncoded(b)` tests the magic (the CLI thereby
  distinguishes a `.rules` from an `.ir.bin` without relying on the extension).
- **gob banned**: non-canonical (field/map ordering, version drift) — incompatible with the
  determinism requirement.
- **Maps** (`Inputs`, `Domains`, `context`) are emitted in **sorted key order**.
- **Decimals** go through `MarshalText`: **exact** text, **without `Reduce`** (no loss of
  precision or scale), arch-independent.

`loader.Compile` migrates to `hex(ir.Hash(cm))`: the identity now reflects the **IR**, not the
text. Two distinct sources that compile to the same IR share the hash (**intended breaking change**:
no test freezes the old source hash).

CLI: `feelc compile --rules x.rules -o x.ir.bin` (displays size + hash); `run`/`verify`/`check`
accept either a source or an `.ir.bin` interchangeably.

## Consequences

- Distribution/execution of a compiled model without the parsing chain.
- Basis for the deterministic goldens (ADR/Slice 19): `modelHash` replayable amd64 + arm64.
- Round-trip proven stable (`Encode→Decode→Encode` bit-for-bit identical); invalid magic rejected
  (never conform silently).
- The `.ir.bin` does **not** carry source positions (`Src`/`Line`/`Col`): `explain` on a binary
  degrades honestly (positions absent).
- **Robustness (untrusted blob).** `Decode` ingests arbitrary `.ir.bin`; the decoder therefore
  bounds (a) every length-prefixed length to the remaining bytes (`count()`) — no giant
  `make(..., n)` from a corrupt size → no OOM; (b) recursion depth
  (`maxDecodeDepth`) — no fatal stack overflow on a deep TagList/Sub nesting.
  Any overrun fails outright (never conform silently). Cf. adversarial review Slice 4.

# IR format and `.ir.bin` serialization

The **IR** (`internal/ir`) is the compiled, immutable model that the VM executes and that
verification analyzes. Three layers: (1) decision graph (DRG); (2) tables normalized into `CellTest`
(an analyzable geometric form); (3) flat bytecode per expression/cell Op=Prog.

## Canonical serialization (`feelc compile -o model.ir.bin`)

Codec: `internal/ir/codec.go` ([ADR 0006](adr/0006-ir-serialization.md)). **Manual,
length-prefixed, big-endian.** `gob` is forbidden (non-canonical → incompatible with
bit-for-bit determinism, the project's thesis).

### Header

```
magic  : 4 octets  "FLIR"
version: uint16     (1)
```

`ir.IsEncoded(b)` tests the magic → the CLI distinguishes a `.rules` source from an `.ir.bin` without
relying on the extension. `run` / `verify` / `check` / `explain` accept both.

### Encoding rules

- integers: big-endian (`uint8`/`uint16`/`uint32`);
- strings / bytes: `uint32` length + bytes;
- booleans: 1 byte;
- **maps** (`Inputs`, `Domains`, `context`): emitted in **sorted key order** (determinism);
- **decimals**: via `MarshalText` (**exact** text, without `Reduce`) → no loss, arch-independent;
- pointers (`Table`, `Expr`, `Prog`): 1 presence byte then the content.

### Robustness (untrusted blob)

`Decode` ingests arbitrary `.ir.bin`: every length is **bounded to the remaining bytes**
(`count()`, no giant `make` → no OOM) and the **recursion depth** is capped
(`maxDecodeDepth`, no stack overflow). Any overflow fails outright.

## Model hash (`ir.Hash`)

`Hash(cm) = sha256(Encode(cm))`: the **canonical identity of the compiled model** (not the source text).
`loader.Compile` exposes it in hex (the service's `hash` field, determinism goldens). **Intended
breaking**: two sources that compile to the same IR share the hash.

## Source positions

`CellTest.Src/Line`, `Rule.Line/OutputSrc`, `Decision.ExprSrc/Line` carry the source trace
(justification for `feelc explain`). A loaded `.ir.bin` preserves these fields (they are serialized);
if they are absent (0/""), `explain` degrades honestly.

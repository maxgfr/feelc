package ir_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
)

func be32(x uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, x)
	return b
}

// valid header + a model whose first Domain.Lo is a Value to decode; we then control
// the bytes of that Value for the malicious blobs below.
func craftToFirstDomainValue() []byte {
	var b []byte
	b = append(b, 'F', 'L', 'I', 'R', 0x00, 0x01) // magic + version 1
	b = append(b, be32(0)...)                     // Name = ""
	b = append(b, be32(0)...)                     // 0 inputs
	b = append(b, be32(1)...)                     // 1 domain
	b = append(b, be32(0)...)                     // domain name = ""
	b = append(b, 0)                              // Domain.Kind
	return b                                      // follows: Lo = getValue(...)
}

// Allocation DoS: a giant length must NEVER trigger a massive make(..., n)
// (adversarial review) — count() bounds it to the remaining bytes and fails hard.
func TestDecodeRejectsHugeLength(t *testing.T) {
	b := craftToFirstDomainValue()
	b = append(b, 5)                   // Lo: tag TagList
	b = append(b, be32(0xFFFFFFFF)...) // count = 4 billion (no byte behind)
	if _, err := ir.Decode(b); err == nil {
		t.Fatal("giant length: Decode should have failed without allocating")
	}
}

// Recursion DoS: deep nesting must not overflow the stack (fatal unrecoverable
// panic); beyond maxDecodeDepth, fail hard.
func TestDecodeRejectsDeepNesting(t *testing.T) {
	b := craftToFirstDomainValue()
	for i := 0; i < 1100; i++ { // > maxDecodeDepth (1000)
		b = append(b, 5)          // TagList
		b = append(b, be32(1)...) // count 1 -> one element (itself nested)
	}
	b = append(b, 0) // innermost value: TagNull
	_, err := ir.Decode(b)
	if err == nil {
		t.Fatal("deep nesting: Decode should have failed (depth guard)")
	}
	if !strings.Contains(err.Error(), "deep") {
		t.Errorf("expected a depth error, got %q", err.Error())
	}
}

// richSrc exercises a maximum of encodable forms: domains (numeric + enum), PRIORITY
// table, multi-column context output, cell Op=Prog, COLLECT sum, literal-expr.
const richSrc = `model "rich" {}
input score : number in [300..850]
input cat : string in { "a", "b" }
input debt : number >= 0
type Out = context { ok: boolean, label: string }
decision dti : number = debt / 12
decision tier : string {
  needs: score, cat
  hit: priority
  priority: "hi", "lo"
  >= 700 | -   => "hi"
  -      | -   => "lo"
}
decision band : Out {
  needs: score
  hit: first
  [300..580) => false | "low"
  -          => true  | "ok"
}
decision flag : boolean {
  needs: score, debt
  hit: first
  < debt | -  => true
  -      | -  => false
}
decision total : number {
  needs: cat
  hit: collect sum
  "a" => 10
  "b" => 20
}
decision big : number = 9007199254740993
`

func compileRich(t *testing.T) *ir.CompiledModel {
	t.Helper()
	m, err := dsl.Parse(richSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cm
}

// Encode->Decode->Encode is bit-for-bit stable (the codec is canonical and deterministic,
// including for exact decimals via MarshalText and the sorted order of maps).
func TestEncodeDecodeRoundTripStable(t *testing.T) {
	cm := compileRich(t)
	enc1, err := ir.Encode(cm)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := ir.Decode(enc1)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	enc2, err := ir.Encode(decoded)
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if !bytes.Equal(enc1, enc2) {
		t.Fatalf("encoding not stable on round-trip (%d vs %d bytes)", len(enc1), len(enc2))
	}
}

// Encode is deterministic across two independent compilations of the SAME source
// (sorted map order → no randomness).
func TestEncodeDeterministicAcrossCompiles(t *testing.T) {
	a, err := ir.Encode(compileRich(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ir.Encode(compileRich(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two compilations of the same source produce different encodings")
	}
}

// Hash is stable and reflects the IR (decoded == original).
func TestHashStableAndReflectsDecoded(t *testing.T) {
	cm := compileRich(t)
	h1, err := ir.Hash(cm)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := ir.Encode(cm)
	decoded, _ := ir.Decode(enc)
	h2, err := ir.Hash(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("original hash %x != decoded hash %x", h1, h2)
	}
}

// A semantic change to the model changes the hash.
func TestHashChangesWithModel(t *testing.T) {
	cm := compileRich(t)
	h1, _ := ir.Hash(cm)

	other := `model "rich2" {}
input score : number in [300..850]
decision band : string {
  needs: score
  hit: first
  >= 700 => "hi"
  -      => "lo"
}
`
	m, _ := dsl.Parse(other)
	cm2, _ := compiler.Compile(m)
	h2, _ := ir.Hash(cm2)
	if h1 == h2 {
		t.Fatal("two different models have the same hash")
	}
}

// A bad magic is rejected cleanly (never conform silently).
func TestDecodeRejectsBadMagic(t *testing.T) {
	if _, err := ir.Decode([]byte("garbage data here")); err == nil {
		t.Fatal("Decode should have rejected a non-feelc blob")
	}
	if _, err := ir.Decode(nil); err == nil {
		t.Fatal("Decode(nil) should have failed")
	}
}

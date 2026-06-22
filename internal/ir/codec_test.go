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

// header valide + un modèle dont la 1re Domain.Lo est une Value à décoder ; on contrôle ensuite
// les octets de cette Value pour les blobs malveillants ci-dessous.
func craftToFirstDomainValue() []byte {
	var b []byte
	b = append(b, 'F', 'L', 'I', 'R', 0x00, 0x01) // magic + version 1
	b = append(b, be32(0)...)                      // Name = ""
	b = append(b, be32(0)...)                      // 0 inputs
	b = append(b, be32(1)...)                      // 1 domain
	b = append(b, be32(0)...)                      // domain name = ""
	b = append(b, 0)                               // Domain.Kind
	return b // suit : Lo = getValue(...)
}

// DoS d'allocation : une longueur géante ne doit JAMAIS déclencher un make(..., n) massif
// (revue adverse) — count() la borne aux octets restants et échoue franchement.
func TestDecodeRejectsHugeLength(t *testing.T) {
	b := craftToFirstDomainValue()
	b = append(b, 5)                   // Lo : tag TagList
	b = append(b, be32(0xFFFFFFFF)...) // count = 4 milliards (aucun octet derrière)
	if _, err := ir.Decode(b); err == nil {
		t.Fatal("longueur géante : Decode aurait dû échouer sans allouer")
	}
}

// DoS de récursion : une imbrication profonde ne doit pas faire déborder la pile (panic fatale
// non rattrapable) ; au-delà de maxDecodeDepth, échec franc.
func TestDecodeRejectsDeepNesting(t *testing.T) {
	b := craftToFirstDomainValue()
	for i := 0; i < 1100; i++ { // > maxDecodeDepth (1000)
		b = append(b, 5)          // TagList
		b = append(b, be32(1)...) // count 1 -> un élément (lui-même imbriqué)
	}
	b = append(b, 0) // valeur la plus interne : TagNull
	_, err := ir.Decode(b)
	if err == nil {
		t.Fatal("imbrication profonde : Decode aurait dû échouer (garde de profondeur)")
	}
	if !strings.Contains(err.Error(), "profonde") {
		t.Errorf("attendu une erreur de profondeur, obtenu %q", err.Error())
	}
}

// richSrc exerce un maximum de formes encodables : domaines (numérique + enum), table
// PRIORITY, sortie context multi-colonnes, cellule Op=Prog, COLLECT sum, literal-expr.
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

// Encode->Decode->Encode est stable bit-à-bit (le codec est canonique et déterministe,
// y compris pour les décimaux exacts via MarshalText et l'ordre trié des maps).
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
		t.Fatalf("encodage non stable au round-trip (%d vs %d octets)", len(enc1), len(enc2))
	}
}

// Encode est déterministe sur deux compilations indépendantes de la MÊME source
// (ordre des maps trié → pas d'aléa).
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
		t.Fatal("deux compilations de la même source produisent des encodages différents")
	}
}

// Hash est stable et reflète l'IR (décodé == original).
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
		t.Fatalf("hash original %x != hash décodé %x", h1, h2)
	}
}

// Une modification sémantique du modèle change le hash.
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
		t.Fatal("deux modèles différents ont le même hash")
	}
}

// Un mauvais magic est rejeté proprement (jamais conformer en silence).
func TestDecodeRejectsBadMagic(t *testing.T) {
	if _, err := ir.Decode([]byte("garbage data here")); err == nil {
		t.Fatal("Decode aurait dû rejeter un blob non-feelc")
	}
	if _, err := ir.Decode(nil); err == nil {
		t.Fatal("Decode(nil) aurait dû échouer")
	}
}

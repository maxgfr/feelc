package engine_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

// Determinism goldens (Slice 19). We freeze, per (model, decision, input):
//   - model_hash : hex(ir.Hash) of the compiled model (canonical identity, ADR 0006);
//   - out_canon  : the output in canonical JSON form (decimals via Text('f'));
//   - out_hash   : sha256(out_canon).
// The test REPLAYS these goldens. In CI they are replayed on amd64 AND arm64 (the
// ubuntu/macos matrix): bit-for-bit cross-platform equality is the project's core thesis.
//
// The expected `out_canon` is additionally HAND-SPECIFIED in each case (ExpectCanon): the
// golden is therefore not regenerated "blindly" — semantic correctness is anchored by
// hand-written values, while the golden only freezes the invariance (hash + platform).
//
// Regeneration: `FEELC_REGEN_GOLDEN=1 go test ./internal/engine -run Golden`.

type goldenCase struct {
	Name        string
	Rules       string // path relative to internal/engine
	Decision    string
	Input       map[string]any
	ExpectCanon string // expected canonical output (correctness anchor, hand-written)
}

type goldenEntry struct {
	ModelHash string `json:"model_hash"`
	OutHash   string `json:"out_hash"`
	OutCanon  string `json:"out_canon"`
}

var goldenCases = []goldenCase{
	{"credit/eligibility/approved", "../../examples/credit/credit.rules", "eligibility",
		map[string]any{"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
		`{"eligible":true,"reason":"approved"}`},
	{"credit/eligibility/low_score", "../../examples/credit/credit.rules", "eligibility",
		map[string]any{"credit_score": 500, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
		`{"eligible":false,"reason":"insufficient score"}`},
	{"credit/dti", "../../examples/credit/credit.rules", "dti",
		map[string]any{"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
		`"0.3"`},
	{"benefits/aids", "../../examples/benefits/benefits.rules", "aids",
		map[string]any{"income": 900, "children": 2, "is_student": true},
		`["housing","family","student_grant"]`},
	{"insurance/surcharge", "../../examples/insurance/insurance.rules", "surcharge",
		map[string]any{"age": 22, "region": "urban", "claims": 4, "base_premium": 1000},
		`"950"`},
	{"insurance/premium", "../../examples/insurance/insurance.rules", "premium",
		map[string]any{"age": 22, "region": "urban", "claims": 4, "base_premium": 1000},
		`"1950"`},
	{"promo/discount", "../../examples/promo/promo.rules", "discount_pct",
		map[string]any{"cart_total": 120, "is_member": true, "promo_code": "BIG20"},
		`"20"`},
}

func loadGoldenModel(t *testing.T, path string) *ir.CompiledModel {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m, err := dsl.Parse(string(src))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatalf("compile %s: %v", path, err)
	}
	return cm
}

// canon renders an output in deterministic form: decimals -> Text('f'), recursively.
func canon(v any) any {
	switch x := v.(type) {
	case *apd.Decimal:
		return x.Text('f')
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = canon(e)
		}
		return m
	case []any:
		xs := make([]any, len(x))
		for i, e := range x {
			xs[i] = canon(e)
		}
		return xs
	default:
		return x
	}
}

func TestGoldenDeterminism(t *testing.T) {
	goldenPath := filepath.Join("testdata", "golden.json")
	regen := os.Getenv("FEELC_REGEN_GOLDEN") != ""

	models := map[string]*ir.CompiledModel{}
	got := map[string]goldenEntry{}
	for _, c := range goldenCases {
		cm := models[c.Rules]
		if cm == nil {
			cm = loadGoldenModel(t, c.Rules)
			models[c.Rules] = cm
		}
		h, err := ir.Hash(cm)
		if err != nil {
			t.Fatalf("%s: hash: %v", c.Name, err)
		}
		out, err := engine.Eval(cm, c.Decision, c.Input)
		if err != nil {
			t.Fatalf("%s: eval: %v", c.Name, err)
		}
		b, err := json.Marshal(canon(out))
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.Name, err)
		}
		// Semantic correctness anchor (hand-specified), independent of the frozen golden.
		if string(b) != c.ExpectCanon {
			t.Errorf("%s: output %s, expected %s", c.Name, b, c.ExpectCanon)
		}
		oh := sha256.Sum256(b)
		got[c.Name] = goldenEntry{
			ModelHash: hex.EncodeToString(h[:]),
			OutHash:   hex.EncodeToString(oh[:]),
			OutCanon:  string(b),
		}
	}

	if regen {
		blob, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(blob, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden regenerated: %d cases in %s", len(got), goldenPath)
		return
	}

	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden missing (%v) — regenerate with FEELC_REGEN_GOLDEN=1", err)
	}
	var want map[string]goldenEntry
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("golden unreadable: %v", err)
	}
	for name, g := range got {
		w, ok := want[name]
		if !ok {
			t.Errorf("case %q absent from golden (regenerate with FEELC_REGEN_GOLDEN=1)", name)
			continue
		}
		if g != w {
			t.Errorf("case %q: determinism broken\n  got  = %+v\n  want = %+v", name, g, w)
		}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("case %q present in golden but absent from cases (clean up the golden)", name)
		}
	}
}

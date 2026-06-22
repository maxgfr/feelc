//go:build smt

package verify_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/verify"
)

// Sous `-tags smt`, le backend SMT est branché : une table à cellule Op=Prog est routée vers lui
// (et non plus le générique "résidu non vérifié"). Selon la présence de z3 : preuve, trou, ou
// dégradation honnête "z3 introuvable" — dans tous les cas le message mentionne SMT/z3.
func TestSMTBackendHandlesOpProg(t *testing.T) {
	// `? < other` référence un autre input -> cellule Op=Prog (non géométrique).
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
input other : number in [0..100]
decision d : string {
  needs: x
  hit: first
  ? < other => "lo"
  -         => "hi"
}`))
	f := has(rep, verify.KindNotVerifiable)
	g := has(rep, verify.KindGap)
	var msg string
	if f != nil {
		msg = f.Message
	} else if g != nil {
		msg = g.Message
	} else {
		t.Fatalf("attendu un finding du backend SMT, rapport=%+v", rep.Findings)
	}
	if !strings.Contains(msg, "SMT") && !strings.Contains(msg, "z3") {
		t.Errorf("le backend SMT aurait dû traiter Op=Prog ; message=%q", msg)
	}
}

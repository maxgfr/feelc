// Spike jetable (Tranche 0) : évaluer empiriquement si github.com/pbinitiative/feel
// peut parser le sous-ensemble FEEL dont feelc a besoin pour les CELLULES de table
// (unary tests) et les EXPRESSIONS littérales. Verdict data-driven pour l'ADR feel-frontend.
//
// On NE teste PAS l'évaluation (leur Number = big.Float binaire, non-décimal) : feelc
// exécutera via apd dans sa propre VM. Ici on teste seulement : « le parseur accepte-t-il
// notre syntaxe, et l'AST exporté est-il exploitable (positions, littéral source) ? »
package main

import (
	"fmt"

	feel "github.com/pbinitiative/feel"
)

type probe struct {
	kind string // "unary" (cellule de table) ou "expr" (décision literal expression)
	src  string
}

func main() {
	probes := []probe{
		// --- Unary tests (cellules de table) : le cœur du moteur DMN ---
		{"unary", `< 580`},
		{"unary", `<= 0.43`},
		{"unary", `> 0.43`},
		{"unary", `>= 680`},
		{"unary", `>= 18`},
		{"unary", `!= 0`},
		{"unary", `[580..680)`},   // range borne droite exclue
		{"unary", `[300..850]`},   // range fermé
		{"unary", `(0..1)`},       // range ouvert
		{"unary", `]0..100]`},     // notation crochet inversé (borne gauche exclue)
		{"unary", `"good"`},       // littéral string = égalité implicite
		{"unary", `"good","excellent"`}, // liste = OU implicite (MultiTests)
		{"unary", `1, 2, 3`},      // liste de nombres
		{"unary", `not(< 18)`},    // négation
		{"unary", `not("none")`},  // négation de littéral
		{"unary", `-`},            // any / don't-care (DMN) : à surveiller (peut = moins unaire)
		{"unary", `580`},          // nombre nu = égalité
		{"unary", `true`},         // booléen
		{"unary", `< monthly_debt`}, // comparaison à une autre variable (cellule Op=Prog)
		{"unary", `[date("2026-01-01")..date("2026-12-31")]`}, // range de dates
		// --- Expressions (décisions literal expression) ---
		{"expr", `monthly_debt / (annual_income / 12)`},
		{"expr", `credit_score * 2 + 10`},
		{"expr", `if age < 18 then "mineur" else "majeur"`},
		{"expr", `annual_income >= 30000 and credit_score >= 700`},
		{"expr", `sum([1, 2, 3])`},
		{"expr", `floor(3.7)`},
		{"expr", `substring("hello", 1, 3)`},
		{"expr", `min(a, b, c)`},
		{"expr", `duration("P30D")`},
		{"expr", `date("2026-06-22")`},
	}

	var okU, totU, okE, totE int
	for _, p := range probes {
		if p.kind == "unary" {
			totU++
		} else {
			totE++
		}
		node, err := feel.ParseString(p.src)
		status := "OK "
		detail := ""
		if err != nil {
			status = "ERR"
			detail = err.Error()
		} else {
			if p.kind == "unary" {
				okU++
			} else {
				okE++
			}
			detail = fmt.Sprintf("%T", node)
			if r, ok := node.(interface{ Repr() string }); ok {
				detail += "  ⟶  " + r.Repr()
			}
		}
		fmt.Printf("[%s] %-4s %-45s %s\n", status, p.kind, p.src, detail)
	}

	fmt.Printf("\n=== BILAN ===\nunary-tests : %d/%d\nexpressions : %d/%d\n", okU, totU, okE, totE)
}

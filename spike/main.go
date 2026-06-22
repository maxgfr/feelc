// Throwaway spike (Slice 0): empirically evaluate whether github.com/pbinitiative/feel
// can parse the FEEL subset feelc needs for table CELLS (unary tests) and literal
// EXPRESSIONS. Data-driven verdict for the feel-frontend ADR.
//
// We do NOT test evaluation (their Number = binary big.Float, non-decimal): feelc
// executes via apd in its own VM. Here we only test: "does the parser accept our
// syntax, and is the exported AST usable (positions, source literal)?"
package main

import (
	"fmt"

	feel "github.com/pbinitiative/feel"
)

type probe struct {
	kind string // "unary" (table cell) or "expr" (literal-expression decision)
	src  string
}

func main() {
	probes := []probe{
		// --- Unary tests (table cells): the heart of the DMN engine ---
		{"unary", `< 580`},
		{"unary", `<= 0.43`},
		{"unary", `> 0.43`},
		{"unary", `>= 680`},
		{"unary", `>= 18`},
		{"unary", `!= 0`},
		{"unary", `[580..680)`},         // range with right bound excluded
		{"unary", `[300..850]`},         // closed range
		{"unary", `(0..1)`},             // open range
		{"unary", `]0..100]`},           // inverted-bracket notation (left bound excluded)
		{"unary", `"good"`},             // string literal = implicit equality
		{"unary", `"good","excellent"`}, // list = implicit OR (MultiTests)
		{"unary", `1, 2, 3`},            // list of numbers
		{"unary", `not(< 18)`},          // negation
		{"unary", `not("none")`},        // literal negation
		{"unary", `-`},                  // any / don't-care (DMN): to watch (may = unary minus)
		{"unary", `580`},                // bare number = equality
		{"unary", `true`},               // boolean
		{"unary", `< monthly_debt`},     // comparison to another variable (Op=Prog cell)
		{"unary", `[date("2026-01-01")..date("2026-12-31")]`}, // range of dates
		// --- Expressions (literal-expression decisions) ---
		{"expr", `monthly_debt / (annual_income / 12)`},
		{"expr", `credit_score * 2 + 10`},
		{"expr", `if age < 18 then "minor" else "adult"`},
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

	fmt.Printf("\n=== SUMMARY ===\nunary-tests : %d/%d\nexpressions : %d/%d\n", okU, totU, okE, totE)
}

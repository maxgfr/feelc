// Package decimal centralise l'arithmétique décimale EXACTE de feelc (cf. ADR 0002).
// Le contexte est FIGÉ (précision 34 = Decimal128, arrondi HALF_EVEN) : c'est une
// condition du déterminisme bit-à-bit inter-plateforme, thèse centrale du produit.
package decimal

import (
	"fmt"

	apd "github.com/cockroachdb/apd/v3"
)

// Ctx est le contexte décimal figé. Ne pas muter à l'exécution.
var Ctx = func() *apd.Context {
	c := apd.BaseContext.WithPrecision(34)
	c.Rounding = apd.RoundHalfEven
	return c
}()

// Parse lit un décimal exact depuis sa représentation source (le littéral du .rules).
func Parse(s string) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		return nil, fmt.Errorf("décimal invalide %q: %w", s, err)
	}
	return d, nil
}

// FromInt construit un décimal à partir d'un entier.
func FromInt(i int64) *apd.Decimal { return apd.New(i, 0) }

// Cmp compare deux décimaux : -1 si a<b, 0 si égaux, 1 si a>b.
func Cmp(a, b *apd.Decimal) int { return a.Cmp(b) }

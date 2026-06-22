// Package decimal centralizes feelc's EXACT decimal arithmetic (see ADR 0002).
// The context is FROZEN (precision 34 = Decimal128, HALF_EVEN rounding): this is a
// condition of cross-platform bit-for-bit determinism, the product's central thesis.
package decimal

import (
	"fmt"

	apd "github.com/cockroachdb/apd/v3"
)

// Ctx is the frozen decimal context. Do not mutate at runtime.
var Ctx = func() *apd.Context {
	c := apd.BaseContext.WithPrecision(34)
	c.Rounding = apd.RoundHalfEven
	return c
}()

// Parse reads an exact decimal from its source representation (the .rules literal).
func Parse(s string) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid decimal %q: %w", s, err)
	}
	return d, nil
}

// FromInt builds a decimal from an integer.
func FromInt(i int64) *apd.Decimal { return apd.New(i, 0) }

// Cmp compares two decimals: -1 if a<b, 0 if equal, 1 if a>b.
func Cmp(a, b *apd.Decimal) int { return a.Cmp(b) }

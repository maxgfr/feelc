// Package units provides a tiny dimensional algebra for feelc's compile-time unit checking. A Unit
// is a multiset of base symbols with integer exponents (e.g. "EUR/month" -> {EUR:1, month:-1}).
// Units are PURELY a compile-time/type concern: runtime values stay plain exact decimals, so the
// deterministic VM is unaffected. Dimensionless is the empty unit.
package units

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Unit maps a base symbol to its (non-zero) exponent. The nil/empty Unit is dimensionless.
type Unit map[string]int

// Parse reads a unit string: numerator and denominator factors separated by "/", factors within a
// side separated by "." or "*", each factor an alphanumeric symbol with an optional "^n" exponent
// (e.g. "EUR", "EUR/month", "kg.m/s^2", "person.year"). Empty string => dimensionless.
func Parse(s string) (Unit, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Unit{}, nil
	}
	parts := strings.Split(s, "/")
	if len(parts) > 2 {
		return nil, fmt.Errorf("unit %q: at most one '/' is allowed", s)
	}
	u := Unit{}
	if err := addSide(u, parts[0], +1); err != nil {
		return nil, err
	}
	if len(parts) == 2 {
		if err := addSide(u, parts[1], -1); err != nil {
			return nil, err
		}
	}
	u.normalize()
	return u, nil
}

func addSide(u Unit, side string, sign int) error {
	side = strings.TrimSpace(side)
	if side == "" || side == "1" {
		return nil
	}
	for _, f := range strings.FieldsFunc(side, func(r rune) bool { return r == '.' || r == '*' }) {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		sym, exp := f, 1
		if i := strings.IndexByte(f, '^'); i >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(f[i+1:]))
			if err != nil {
				return fmt.Errorf("unit factor %q: bad exponent", f)
			}
			sym, exp = strings.TrimSpace(f[:i]), n
		}
		if !validSymbol(sym) {
			return fmt.Errorf("unit factor %q: symbol must be alphanumeric", f)
		}
		u[sym] += sign * exp
	}
	return nil
}

func validSymbol(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

func (u Unit) normalize() {
	for k, v := range u {
		if v == 0 {
			delete(u, k)
		}
	}
}

// IsZero reports whether the unit is dimensionless.
func (u Unit) IsZero() bool { return len(u) == 0 }

// Equal reports dimensional equality.
func (u Unit) Equal(v Unit) bool {
	if len(u) != len(v) {
		return false
	}
	for k, e := range u {
		if v[k] != e {
			return false
		}
	}
	return true
}

// Mul returns the product of two units (exponents add).
func (u Unit) Mul(v Unit) Unit { return combine(u, v, +1) }

// Div returns the quotient (exponents subtract).
func (u Unit) Div(v Unit) Unit { return combine(u, v, -1) }

func combine(u, v Unit, sign int) Unit {
	out := Unit{}
	for k, e := range u {
		out[k] += e
	}
	for k, e := range v {
		out[k] += sign * e
	}
	out.normalize()
	return out
}

// String renders the canonical form: numerator factors (sorted) joined by ".", then "/" and the
// denominator factors. Dimensionless => "".
func (u Unit) String() string {
	if len(u) == 0 {
		return ""
	}
	var num, den []string
	keys := make([]string, 0, len(u))
	for k := range u {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := u[k]
		if e > 0 {
			num = append(num, factor(k, e))
		} else {
			den = append(den, factor(k, -e))
		}
	}
	n := "1"
	if len(num) > 0 {
		n = strings.Join(num, ".")
	}
	if len(den) == 0 {
		return n
	}
	return n + "/" + strings.Join(den, ".")
}

func factor(sym string, exp int) string {
	if exp == 1 {
		return sym
	}
	return sym + "^" + strconv.Itoa(exp)
}

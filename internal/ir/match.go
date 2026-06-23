package ir

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/decimal"
)

// Matching semantics SHARED between the VM (execution) and the checker (analysis).
// A single source of truth -> execution and proof agree by construction.

// ValueEq: typed equality of two values (null == null).
func ValueEq(a, b Value) bool {
	if a.Tag != b.Tag {
		return false
	}
	switch a.Tag {
	case TagNumber, TagDate, TagDuration:
		return decimal.Cmp(a.Num, b.Num) == 0
	case TagString:
		return a.Str == b.Str
	case TagBool:
		return a.Bool == b.Bool
	case TagNull, TagNA:
		return true
	}
	return false
}

// comparableNum reports whether a tag carries an ordered numeric magnitude in Num (number, date,
// duration — all day/decimal integers internally).
func comparableNum(t Tag) bool {
	return t == TagNumber || t == TagDate || t == TagDuration
}

// NumCompare: numeric comparison. null satisfies no comparison (three-valued, ADR 0003).
func NumCompare(op Op, v, a Value) (bool, error) {
	if v.Tag == TagNull || v.Tag == TagNA {
		return false, nil
	}
	if !comparableNum(v.Tag) || v.Tag != a.Tag {
		return false, fmt.Errorf("comparison requires two values of the same number/date/duration type")
	}
	c := decimal.Cmp(v.Num, a.Num)
	switch op {
	case OpLt:
		return c < 0, nil
	case OpLe:
		return c <= 0, nil
	case OpGt:
		return c > 0, nil
	case OpGe:
		return c >= 0, nil
	}
	return false, fmt.Errorf("invalid comparison operator")
}

func inRange(ct CellTest, v Value) (bool, error) {
	if v.Tag == TagNull || v.Tag == TagNA {
		return false, nil
	}
	if v.Tag != TagNumber || ct.A.Tag != TagNumber || ct.B.Tag != TagNumber {
		return false, fmt.Errorf("range on a non-numeric value")
	}
	lo := decimal.Cmp(v.Num, ct.A.Num)
	hi := decimal.Cmp(v.Num, ct.B.Num)
	okLo := lo > 0 || (lo == 0 && !ct.AOpen)
	okHi := hi < 0 || (hi == 0 && !ct.BOpen)
	return okLo && okHi, nil
}

// MatchCell: does cell `ct` match value `v`?
// OpProg requires bytecode evaluation (non-geometric) -> error here; the VM handles it.
// `Negate` (from `not(<test>)`) inverts the geometric test. null stays non-matching even
// when negated (three-valued, ADR 0003: null satisfies no test, and `not` does not "wake" it).
func MatchCell(ct CellTest, v Value) (bool, error) {
	res, err := matchCellBase(ct, v)
	if err != nil {
		return false, err
	}
	if ct.Negate {
		if v.Tag == TagNull || v.Tag == TagNA {
			return false, nil
		}
		return !res, nil
	}
	return res, nil
}

func matchCellBase(ct CellTest, v Value) (bool, error) {
	switch ct.Op {
	case OpAny:
		return true, nil
	case OpEq:
		return ValueEq(v, ct.A), nil
	case OpNe:
		return !ValueEq(v, ct.A), nil
	case OpLt, OpLe, OpGt, OpGe:
		return NumCompare(ct.Op, v, ct.A)
	case OpInRange:
		return inRange(ct, v)
	case OpInSet:
		for _, sub := range ct.Sub {
			ok, err := MatchCell(sub, v)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case OpProg:
		return false, fmt.Errorf("cell Op=Prog requires evaluation (non-geometric)")
	default:
		return false, fmt.Errorf("unknown cell operator: %d", ct.Op)
	}
}

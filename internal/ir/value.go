package ir

import (
	"encoding/json"
	"fmt"
	"strconv"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
)

// Tag identifies the dynamic type of a Value (FEEL is three-valued: null is part of the set).
type Tag uint8

const (
	TagNull Tag = iota
	TagNumber
	TagString
	TagBool
	TagContext // multi-column output of a decision (DMN context)
	TagList    // list (raw COLLECT / RULE ORDER)
)

// Value: fixed-size unboxed value manipulated by the VM.
// No interface{} in the hot path (cf. plan). Numbers are exact decimals.
type Value struct {
	Tag  Tag
	Num  *apd.Decimal
	Str  string
	Bool bool
	Ctx  map[string]Value // if TagContext
	List []Value          // if TagList
}

func Null() Value                  { return Value{Tag: TagNull} }
func Num(d *apd.Decimal) Value     { return Value{Tag: TagNumber, Num: d} }
func Str(s string) Value           { return Value{Tag: TagString, Str: s} }
func Bool(b bool) Value            { return Value{Tag: TagBool, Bool: b} }
func Ctx(m map[string]Value) Value { return Value{Tag: TagContext, Ctx: m} }
func List(xs []Value) Value        { return Value{Tag: TagList, List: xs} }

// FromAny converts an external input (JSON-ish map) into a deterministic Value.
// Numbers go back through their decimal representation to stay exact.
func FromAny(v any) (Value, error) {
	switch x := v.(type) {
	case nil:
		return Null(), nil
	case string:
		return Str(x), nil
	case bool:
		return Bool(x), nil
	case int:
		return Num(decimal.FromInt(int64(x))), nil
	case int64:
		return Num(decimal.FromInt(x)), nil
	case float64:
		d, err := decimal.Parse(strconv.FormatFloat(x, 'f', -1, 64))
		if err != nil {
			return Value{}, err
		}
		return Num(d), nil
	case json.Number:
		// JSON input decoded with UseNumber: we keep the exact repr (no detour through float64).
		d, err := decimal.Parse(x.String())
		if err != nil {
			return Value{}, err
		}
		return Num(d), nil
	case *apd.Decimal:
		return Num(x), nil
	default:
		return Value{}, fmt.Errorf("unsupported input type: %T", v)
	}
}

// ToAny renders the Value in a form usable outside the engine.
func (v Value) ToAny() any {
	switch v.Tag {
	case TagNumber:
		// Canonical external representation: we strip the trailing zeros produced by
		// divisions (0.3000... -> 0.3) without mutating the internal value. The value is unchanged.
		out := new(apd.Decimal)
		out.Reduce(v.Num)
		return out
	case TagString:
		return v.Str
	case TagBool:
		return v.Bool
	case TagContext:
		m := make(map[string]any, len(v.Ctx))
		for k, f := range v.Ctx {
			m[k] = f.ToAny()
		}
		return m
	case TagList:
		xs := make([]any, len(v.List))
		for i, e := range v.List {
			xs[i] = e.ToAny()
		}
		return xs
	default:
		return nil
	}
}

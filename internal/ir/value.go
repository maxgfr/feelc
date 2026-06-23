package ir

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

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
	TagNA      // non-applicable (distinct from null): a rule that does not apply (ADR 0013).
	// Encoded tag-only (like TagNull), so adding it needs no codec version bump.
	TagDate     // calendar date (ADR 0014): integer days since the Unix epoch, stored in Num.
	TagDuration // whole-day duration (ADR 0014): integer days, stored in Num.
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

func Null() Value { return Value{Tag: TagNull} }
func NA() Value   { return Value{Tag: TagNA} }

// Date/Duration store an integer day-count (date = days since the Unix epoch) as an exact decimal
// in Num, so date arithmetic reuses the decimal engine and stays deterministic (ADR 0014).
func Date(days int64) Value     { return Value{Tag: TagDate, Num: decimal.FromInt(days)} }
func Duration(days int64) Value { return Value{Tag: TagDuration, Num: decimal.FromInt(days)} }

const secondsPerDay = 86400

// ParseDate parses an ISO "YYYY-MM-DD" date into a Value (days since the Unix epoch, UTC).
func ParseDate(s string) (Value, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Value{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", s)
	}
	return Date(t.UTC().Unix() / secondsPerDay), nil
}

// ParseDuration parses an ISO whole-day duration "P<n>D" (e.g. "P30D", "-P7D") into a Value.
func ParseDuration(s string) (Value, error) {
	neg := false
	t := s
	if len(t) > 0 && t[0] == '-' {
		neg, t = true, t[1:]
	}
	if len(t) < 3 || t[0] != 'P' || t[len(t)-1] != 'D' {
		return Value{}, fmt.Errorf("invalid duration %q (want P<days>D, e.g. P30D)", s)
	}
	n, err := strconv.ParseInt(t[1:len(t)-1], 10, 64)
	if err != nil {
		return Value{}, fmt.Errorf("invalid duration %q (want P<days>D, e.g. P30D)", s)
	}
	if neg {
		n = -n
	}
	return Duration(n), nil
}

// CoerceInputs converts string inputs declared as `date`/`duration` into typed temporal Values
// (callers pass JSON strings; the declared type decides the parse). Mutates the map in place. A
// present-but-non-string temporal input is a loud error (never silently mis-typed); null is left as
// null (three-valued semantics, ADR 0003).
func CoerceInputs(cm *CompiledModel, inputs map[string]Value) error {
	for name, t := range cm.Inputs {
		if t != TypeDate && t != TypeDuration {
			continue
		}
		v, ok := inputs[name]
		if !ok || v.Tag == TagNull {
			continue
		}
		if v.Tag != TagString {
			return fmt.Errorf("input %q: expected an ISO %s string", name, typeWord(t))
		}
		var d Value
		var err error
		if t == TypeDate {
			d, err = ParseDate(v.Str)
		} else {
			d, err = ParseDuration(v.Str)
		}
		if err != nil {
			return fmt.Errorf("input %q: %w", name, err)
		}
		inputs[name] = d
	}
	return nil
}

func typeWord(t Type) string {
	if t == TypeDuration {
		return "duration"
	}
	return "date"
}

// dayInt extracts the integer day-count of a date/duration Value.
func (v Value) dayInt() int64 {
	if v.Num == nil {
		return 0
	}
	n, _ := v.Num.Int64()
	return n
}
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
	case TagNA:
		return NotApplicable{}
	case TagDate:
		return time.Unix(v.dayInt()*secondsPerDay, 0).UTC().Format("2006-01-02")
	case TagDuration:
		d := v.dayInt()
		if d < 0 {
			return fmt.Sprintf("-P%dD", -d)
		}
		return fmt.Sprintf("P%dD", d)
	default:
		return nil
	}
}

// NotApplicable is the external rendering of a non-applicable (TagNA) result. It marshals to the
// JSON string "non-applicable" and prints likewise, so it is distinguishable from null everywhere.
type NotApplicable struct{}

func (NotApplicable) MarshalJSON() ([]byte, error) { return []byte(`"non-applicable"`), nil }
func (NotApplicable) String() string               { return "non-applicable" }

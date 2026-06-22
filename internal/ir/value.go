package ir

import (
	"encoding/json"
	"fmt"
	"strconv"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
)

// Tag identifie le type dynamique d'une Value (FEEL est trivalent : null fait partie du jeu).
type Tag uint8

const (
	TagNull Tag = iota
	TagNumber
	TagString
	TagBool
	TagContext // sortie multi-colonnes d'une décision (DMN context)
	TagList    // liste (COLLECT brut / RULE ORDER)
)

// Value : valeur unboxée de taille fixe manipulée par la VM.
// Pas d'interface{} dans le hot path (cf. plan). Les nombres sont décimaux exacts.
type Value struct {
	Tag  Tag
	Num  *apd.Decimal
	Str  string
	Bool bool
	Ctx  map[string]Value // si TagContext
	List []Value          // si TagList
}

func Null() Value             { return Value{Tag: TagNull} }
func Num(d *apd.Decimal) Value { return Value{Tag: TagNumber, Num: d} }
func Str(s string) Value      { return Value{Tag: TagString, Str: s} }
func Bool(b bool) Value       { return Value{Tag: TagBool, Bool: b} }
func Ctx(m map[string]Value) Value { return Value{Tag: TagContext, Ctx: m} }
func List(xs []Value) Value         { return Value{Tag: TagList, List: xs} }

// FromAny convertit une entrée externe (map JSON-ish) en Value déterministe.
// Les nombres repassent par leur représentation décimale pour rester exacts.
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
		// Entrée JSON décodée avec UseNumber : on garde la repr exacte (pas de passage par float64).
		d, err := decimal.Parse(x.String())
		if err != nil {
			return Value{}, err
		}
		return Num(d), nil
	case *apd.Decimal:
		return Num(x), nil
	default:
		return Value{}, fmt.Errorf("type d'entrée non supporté: %T", v)
	}
}

// ToAny rend la Value sous une forme exploitable hors du moteur.
func (v Value) ToAny() any {
	switch v.Tag {
	case TagNumber:
		// Représentation externe canonique : on retire les zéros de traîne issus des
		// divisions (0.3000... -> 0.3) sans muter la valeur interne. La valeur est inchangée.
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

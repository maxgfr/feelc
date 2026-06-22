// Package compiler transforme un *model.Model (sortie du DSL) en *ir.CompiledModel
// exécutable : typecheck (gardien du périmètre) + lowering (normalisation des cellules
// en CellTest). En Tranche 1 : tables, hit policy FIRST, sorties littérales, cellules
// any/comparaison/égalité. Tout le reste échoue franchement.
package compiler

import (
	"fmt"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/model"
)

// Compile typecheck puis abaisse le modèle conceptuel en IR.
func Compile(m *model.Model) (*ir.CompiledModel, error) {
	cm := &ir.CompiledModel{Name: m.Name, Inputs: map[string]ir.Type{}}
	for _, in := range m.Inputs {
		cm.Inputs[in.Name] = irType(in.Type)
	}
	for _, d := range m.Decisions {
		dec, err := compileDecision(cm, d)
		if err != nil {
			return nil, err
		}
		cm.Decisions = append(cm.Decisions, dec)
	}
	return cm, nil
}

func compileDecision(cm *ir.CompiledModel, d model.Decision) (ir.Decision, error) {
	hp, err := parseHitPolicy(d.HitPolicy, d.Line)
	if err != nil {
		return ir.Decision{}, err
	}
	// Vérifier que chaque `needs` référence une entrée déclarée (résolution DRG triviale en T1).
	for _, n := range d.Needs {
		if _, ok := cm.Inputs[n]; !ok {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): `needs` référence %q, non déclaré en `input`",
				d.Name, d.Line, n)
		}
	}
	table := &ir.DecisionTable{
		Inputs:    d.Needs,
		Outputs:   []string{d.Name}, // 1 sortie nommée comme la décision en T1
		HitPolicy: hp,
	}
	for _, r := range d.Rules {
		if len(r.Conds) != len(d.Needs) {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): %d conditions pour %d colonnes `needs`",
				d.Name, r.Line, len(r.Conds), len(d.Needs))
		}
		if len(r.Outputs) != 1 {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): %d sorties, 1 attendue en v1",
				d.Name, r.Line, len(r.Outputs))
		}
		rule := ir.Rule{}
		for i, c := range r.Conds {
			ct, err := normalizeCell(c, cm.Inputs[d.Needs[i]])
			if err != nil {
				return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): %w", d.Name, r.Line, err)
			}
			rule.Conds = append(rule.Conds, ct)
		}
		out, err := literalValue(r.Outputs[0].Node)
		if err != nil {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): sortie: %w", d.Name, r.Line, err)
		}
		rule.Outputs = []ir.Value{out}
		table.Rules = append(table.Rules, rule)
	}
	return ir.Decision{Name: d.Name, Kind: ir.KindTable, Table: table, Deps: d.Needs}, nil
}

// normalizeCell transforme une cellule de condition en CellTest géométrique.
func normalizeCell(c model.Cell, _ ir.Type) (ir.CellTest, error) {
	if c.Dash {
		return ir.CellTest{Op: ir.OpAny}, nil
	}
	switch n := c.Node.(type) {
	case *feel.Binop:
		// "< 580" est parsé en Binop{Left: Var{"?"}, Op, Right}.
		if v, ok := n.Left.(*feel.Var); ok && v.Name == "?" {
			op, err := mapOp(n.Op)
			if err != nil {
				return ir.CellTest{}, err
			}
			a, err := literalValue(n.Right)
			if err != nil {
				return ir.CellTest{}, fmt.Errorf("comparaison %q: %w", c.Src, err)
			}
			return ir.CellTest{Op: op, A: a}, nil
		}
		return ir.CellTest{}, fmt.Errorf("cellule %q: expression non supportée en v1", c.Src)
	case *feel.NumberNode, *feel.StringNode, *feel.BoolNode:
		a, err := literalValue(c.Node)
		if err != nil {
			return ir.CellTest{}, err
		}
		return ir.CellTest{Op: ir.OpEq, A: a}, nil
	default:
		return ir.CellTest{}, fmt.Errorf("cellule %q: construct non supporté en v1 (prévu plus tard)", c.Src)
	}
}

// literalValue convertit un nœud FEEL littéral en Value (re-parse exact des nombres via apd).
func literalValue(node feel.Node) (ir.Value, error) {
	switch n := node.(type) {
	case *feel.NumberNode:
		d, err := decimal.Parse(n.Value)
		if err != nil {
			return ir.Value{}, err
		}
		return ir.Num(d), nil
	case *feel.StringNode:
		return ir.Str(n.Content()), nil // Content() retire les guillemets + déséchappe (Value les garde)
	case *feel.BoolNode:
		return ir.Bool(n.Value), nil
	default:
		return ir.Value{}, fmt.Errorf("littéral attendu, obtenu %T", node)
	}
}

func mapOp(op string) (ir.Op, error) {
	switch op {
	case "<":
		return ir.OpLt, nil
	case "<=":
		return ir.OpLe, nil
	case ">":
		return ir.OpGt, nil
	case ">=":
		return ir.OpGe, nil
	case "=":
		return ir.OpEq, nil
	case "!=":
		return ir.OpNe, nil
	default:
		return 0, fmt.Errorf("opérateur de cellule non supporté en v1: %q", op)
	}
}

func parseHitPolicy(s string, no int) (ir.HitPolicy, error) {
	switch s {
	case "first":
		return ir.HitFirst, nil
	case "":
		return 0, fmt.Errorf("ligne %d: `hit:` manquant", no)
	default:
		return 0, fmt.Errorf("ligne %d: hit policy non supportée en v1: %q", no, s)
	}
}

func irType(t model.Type) ir.Type {
	switch t {
	case model.TypeString:
		return ir.TypeString
	case model.TypeBool:
		return ir.TypeBool
	default:
		return ir.TypeNumber
	}
}

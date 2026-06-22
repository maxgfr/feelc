// Package compiler transforme un *model.Model (sortie du DSL) en *ir.CompiledModel
// exécutable : typecheck (gardien du périmètre) + lowering (normalisation des cellules
// en CellTest, compilation des expressions en bytecode).
//
// Discipline anti-scope-creep : tout construct hors sous-ensemble v2 échoue franchement.
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
	// Noms résolvables : entrées externes + décisions (une cellule/expr peut référencer une décision amont).
	valid := map[string]bool{}
	for n := range cm.Inputs {
		valid[n] = true
	}
	for _, d := range m.Decisions {
		valid[d.Name] = true
	}
	for _, d := range m.Decisions {
		dec, err := compileDecision(m, valid, d)
		if err != nil {
			return nil, err
		}
		cm.Decisions = append(cm.Decisions, dec)
	}
	return cm, nil
}

func compileDecision(m *model.Model, valid map[string]bool, d model.Decision) (ir.Decision, error) {
	if d.Expr != nil {
		prog, err := lowerExpr(d.Expr.Node)
		if err != nil {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): %w", d.Name, d.Line, err)
		}
		if err := checkVars(prog.Vars, valid, d.Name); err != nil {
			return ir.Decision{}, err
		}
		return ir.Decision{Name: d.Name, Kind: ir.KindLiteralExpr, Expr: prog, Deps: prog.Vars}, nil
	}

	hp, agg, err := parseHitPolicy(d.HitPolicy, d.Line)
	if err != nil {
		return ir.Decision{}, err
	}
	outNames, err := outputNames(m, d)
	if err != nil {
		return ir.Decision{}, err
	}
	if agg != ir.AggNone && agg != ir.AggCount && len(outNames) != 1 {
		return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): l'agrégation COLLECT exige une sortie scalaire unique",
			d.Name, d.Line)
	}
	for _, n := range d.Needs {
		if !valid[n] {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): `needs` référence %q, non déclaré",
				d.Name, d.Line, n)
		}
	}
	table := &ir.DecisionTable{Inputs: d.Needs, Outputs: outNames, HitPolicy: hp, Agg: agg}
	if hp == ir.HitPriority {
		if len(outNames) != 1 {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): PRIORITY exige une sortie scalaire unique", d.Name, d.Line)
		}
		if len(d.Priority) == 0 {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): PRIORITY exige une ligne `priority:` listant les sorties par ordre de priorité décroissant", d.Name, d.Line)
		}
		for _, c := range d.Priority {
			v, err := literalValue(c.Node)
			if err != nil {
				return ir.Decision{}, fmt.Errorf("décision %q: valeur de priorité %q: %w", d.Name, c.Src, err)
			}
			table.Priority = append(table.Priority, v)
		}
	}
	for _, r := range d.Rules {
		outs, err := literalOutputs(r.Outputs, len(outNames), d.Name, r.Line)
		if err != nil {
			return ir.Decision{}, err
		}
		if r.IsDefault {
			table.Default = outs
			continue
		}
		if len(r.Conds) != len(d.Needs) {
			return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): %d conditions pour %d colonnes `needs`",
				d.Name, r.Line, len(r.Conds), len(d.Needs))
		}
		rule := ir.Rule{Outputs: outs}
		for _, c := range r.Conds {
			ct, err := normalizeCell(c, valid, d.Name)
			if err != nil {
				return ir.Decision{}, fmt.Errorf("décision %q (ligne %d): %w", d.Name, r.Line, err)
			}
			rule.Conds = append(rule.Conds, ct)
		}
		table.Rules = append(table.Rules, rule)
	}
	return ir.Decision{Name: d.Name, Kind: ir.KindTable, Table: table, Deps: d.Needs}, nil
}

// outputNames déduit les colonnes de sortie du type de la décision :
// builtin scalaire -> 1 sortie (nom = décision) ; type context -> les champs, dans l'ordre.
func outputNames(m *model.Model, d model.Decision) ([]string, error) {
	switch model.Type(d.TypeName) {
	case model.TypeNumber, model.TypeString, model.TypeBool:
		return []string{d.Name}, nil
	}
	td, ok := m.Type(d.TypeName)
	if !ok {
		return nil, fmt.Errorf("décision %q (ligne %d): type inconnu %q", d.Name, d.Line, d.TypeName)
	}
	names := make([]string, len(td.Fields))
	for i, f := range td.Fields {
		names[i] = f.Name
	}
	return names, nil
}

func literalOutputs(cells []model.Cell, want int, dec string, line int) ([]ir.Value, error) {
	if len(cells) != want {
		return nil, fmt.Errorf("décision %q (ligne %d): %d sorties, %d attendue(s)", dec, line, len(cells), want)
	}
	out := make([]ir.Value, len(cells))
	for i, c := range cells {
		v, err := literalValue(c.Node)
		if err != nil {
			return nil, fmt.Errorf("décision %q (ligne %d): sortie %q: %w", dec, line, c.Src, err)
		}
		out[i] = v
	}
	return out, nil
}

// normalizeCell transforme une cellule de condition en CellTest géométrique (ou Op=Prog).
func normalizeCell(c model.Cell, valid map[string]bool, dec string) (ir.CellTest, error) {
	if c.Dash {
		return ir.CellTest{Op: ir.OpAny}, nil
	}
	return normalizeNode(c.Node, c.Src, valid, dec)
}

func normalizeNode(node feel.Node, src string, valid map[string]bool, dec string) (ir.CellTest, error) {
	switch n := node.(type) {
	case *feel.RangeNode:
		lo, err := literalValue(n.Start)
		if err != nil {
			return ir.CellTest{}, fmt.Errorf("borne basse de %q: %w", src, err)
		}
		hi, err := literalValue(n.End)
		if err != nil {
			return ir.CellTest{}, fmt.Errorf("borne haute de %q: %w", src, err)
		}
		return ir.CellTest{Op: ir.OpInRange, A: lo, B: hi, AOpen: n.StartOpen, BOpen: n.EndOpen}, nil
	case *feel.MultiTests:
		ct := ir.CellTest{Op: ir.OpInSet}
		for _, el := range n.Elements {
			sub, err := normalizeNode(el, src, valid, dec)
			if err != nil {
				return ir.CellTest{}, err
			}
			ct.Sub = append(ct.Sub, sub)
		}
		return ct, nil
	case *feel.Binop:
		if v, ok := n.Left.(*feel.Var); ok && v.Name == "?" {
			if lit, err := literalValue(n.Right); err == nil {
				op, err := mapOp(n.Op)
				if err != nil {
					return ir.CellTest{}, err
				}
				return ir.CellTest{Op: op, A: lit}, nil
			}
			// "? op <expr non littérale>" (ex: "< monthly_debt") -> cellule Op=Prog
			return progCell(node, valid, dec)
		}
		return progCell(node, valid, dec)
	case *feel.NumberNode, *feel.StringNode, *feel.BoolNode:
		lit, err := literalValue(node)
		if err != nil {
			return ir.CellTest{}, err
		}
		return ir.CellTest{Op: ir.OpEq, A: lit}, nil
	default:
		return ir.CellTest{}, fmt.Errorf("cellule %q: construct non supporté en v2", src)
	}
}

// progCell compile une cellule en expression booléenne (couche bytecode), `?` = valeur de colonne.
func progCell(node feel.Node, valid map[string]bool, dec string) (ir.CellTest, error) {
	prog, err := lowerExpr(node)
	if err != nil {
		return ir.CellTest{}, err
	}
	if err := checkVars(prog.Vars, valid, dec); err != nil {
		return ir.CellTest{}, err
	}
	return ir.CellTest{Op: ir.OpProg, Prog: prog}, nil
}

func checkVars(vars []string, valid map[string]bool, dec string) error {
	for _, v := range vars {
		if !valid[v] {
			return fmt.Errorf("décision %q: référence %q, non déclarée (input ou décision)", dec, v)
		}
	}
	return nil
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
		return 0, fmt.Errorf("opérateur de cellule non supporté en v2: %q", op)
	}
}

func parseHitPolicy(s string, no int) (ir.HitPolicy, ir.Aggregation, error) {
	switch s {
	case "":
		return 0, 0, fmt.Errorf("ligne %d: `hit:` manquant", no)
	case "first":
		return ir.HitFirst, ir.AggNone, nil
	case "unique":
		return ir.HitUnique, ir.AggNone, nil
	case "any":
		return ir.HitAny, ir.AggNone, nil
	case "priority":
		return ir.HitPriority, ir.AggNone, nil
	case "rule order":
		return ir.HitRuleOrder, ir.AggNone, nil
	case "collect":
		return ir.HitCollect, ir.AggNone, nil
	case "collect sum":
		return ir.HitCollect, ir.AggSum, nil
	case "collect min":
		return ir.HitCollect, ir.AggMin, nil
	case "collect max":
		return ir.HitCollect, ir.AggMax, nil
	case "collect count":
		return ir.HitCollect, ir.AggCount, nil
	default:
		return 0, 0, fmt.Errorf("ligne %d: hit policy non supportée: %q", no, s)
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

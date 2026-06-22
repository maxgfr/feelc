// Package compiler transforme un *model.Model (sortie du DSL) en *ir.CompiledModel
// exécutable : typecheck (gardien du périmètre) + lowering (normalisation des cellules
// en CellTest, compilation des expressions en bytecode).
//
// Discipline anti-scope-creep : tout construct hors sous-ensemble v2 échoue franchement.
// Les erreurs sont des *diag.Error positionnés (ligne, et colonne quand issue d'une cellule),
// avec un code stable CMPxxx et une suggestion quand c'est utile.
package compiler

import (
	"fmt"
	"sort"
	"strings"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/model"
)

// Compile typecheck puis abaisse le modèle conceptuel en IR.
func Compile(m *model.Model) (*ir.CompiledModel, error) {
	cm := &ir.CompiledModel{Name: m.Name, Inputs: map[string]ir.Type{}, Domains: map[string]ir.Domain{}}
	for _, in := range m.Inputs {
		cm.Inputs[in.Name] = irType(in.Type)
		dom, err := parseDomain(in.Domain)
		if err != nil {
			return nil, diag.Wrap(diag.CodeInputSyntax, in.Line, fmt.Sprintf("input %q", in.Name), err)
		}
		cm.Domains[in.Name] = dom
	}
	// Noms résolvables : entrées externes + décisions (une cellule/expr peut référencer une décision amont).
	// Les BKM ne sont PAS des noms résolvables (pas dans `valid`) : ce sont des fonctions pures,
	// référençables uniquement par invocation `name(...)`, inlinées par le lowerer.
	valid := map[string]bool{}
	for n := range cm.Inputs {
		valid[n] = true
	}
	for _, d := range m.Decisions {
		valid[d.Name] = true
	}
	bkms := map[string]model.BKM{}
	for _, b := range m.BKMs {
		bkms[b.Name] = b
	}
	for _, d := range m.Decisions {
		dec, err := compileDecision(m, valid, bkms, d)
		if err != nil {
			return nil, err
		}
		cm.Decisions = append(cm.Decisions, dec)
	}
	return cm, nil
}

func compileDecision(m *model.Model, valid map[string]bool, bkms map[string]model.BKM, d model.Decision) (ir.Decision, error) {
	if d.Expr != nil {
		prog, err := lowerExpr(d.Expr.Node, bkms)
		if err != nil {
			return ir.Decision{}, diag.Wrap(diag.CodeUnsupported, d.Line, fmt.Sprintf("décision %q", d.Name), err).
				WithCol(d.Expr.Col)
		}
		// `?` (valeur de colonne) n'a de sens que dans une cellule de table : refus FRANC à la
		// compilation (sinon échec seulement à l'exécution — y compris via un argument de BKM).
		if progUsesInput(prog) {
			return ir.Decision{}, diag.Newf(diag.CodeUnsupported, d.Line,
				"décision %q: `?` (valeur de colonne) interdit dans une expression literal — réservé aux cellules de table", d.Name).
				WithCol(d.Expr.Col)
		}
		if err := checkVars(prog.Vars, valid, d.Name, d.Line); err != nil {
			return ir.Decision{}, err
		}
		return ir.Decision{Name: d.Name, Kind: ir.KindLiteralExpr, Expr: prog, ExprSrc: d.Expr.Src, Deps: prog.Vars, Line: d.Line}, nil
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
		return ir.Decision{}, diag.Newf(diag.CodeCollect, d.Line,
			"décision %q: l'agrégation COLLECT exige une sortie scalaire unique", d.Name)
	}
	for _, n := range d.Needs {
		if !valid[n] {
			return ir.Decision{}, diag.Newf(diag.CodeUndeclared, d.Line,
				"décision %q: `needs` référence %q, non déclaré", d.Name, n).
				WithSuggestion("noms déclarés : " + strings.Join(sortedKeys(valid), ", "))
		}
	}
	table := &ir.DecisionTable{Inputs: d.Needs, Outputs: outNames, HitPolicy: hp, Agg: agg}
	if hp == ir.HitPriority {
		if len(outNames) != 1 {
			return ir.Decision{}, diag.Newf(diag.CodePriority, d.Line,
				"décision %q: PRIORITY exige une sortie scalaire unique", d.Name)
		}
		if len(d.Priority) == 0 {
			return ir.Decision{}, diag.Newf(diag.CodePriority, d.Line,
				"décision %q: PRIORITY exige une ligne `priority:` listant les sorties par ordre de priorité décroissant", d.Name)
		}
		for _, c := range d.Priority {
			v, err := literalValue(c.Node)
			if err != nil {
				return ir.Decision{}, diag.Wrap(diag.CodeLiteral, c.Line,
					fmt.Sprintf("décision %q: valeur de priorité %q", d.Name, c.Src), err).WithCol(c.Col)
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
			return ir.Decision{}, diag.Newf(diag.CodeArity, r.Line,
				"décision %q: %d conditions pour %d colonnes `needs`", d.Name, len(r.Conds), len(d.Needs))
		}
		rule := ir.Rule{Outputs: outs, Line: r.Line, OutputSrc: outputSrcs(r.Outputs)}
		for _, c := range r.Conds {
			ct, err := normalizeCell(c, valid, bkms, d.Name)
			if err != nil {
				return ir.Decision{}, err
			}
			rule.Conds = append(rule.Conds, ct)
		}
		table.Rules = append(table.Rules, rule)
	}
	return ir.Decision{Name: d.Name, Kind: ir.KindTable, Table: table, Deps: d.Needs, Line: d.Line}, nil
}

// outputSrcs extrait le texte source des cellules de sortie (trace de justification).
func outputSrcs(cells []model.Cell) []string {
	srcs := make([]string, len(cells))
	for i, c := range cells {
		srcs[i] = c.Src
	}
	return srcs
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
		return nil, diag.Newf(diag.CodeUnknownType2, d.Line, "décision %q: type inconnu %q", d.Name, d.TypeName).
			WithSuggestion("types : number, string, boolean, ou un `type ... = context { ... }` déclaré")
	}
	names := make([]string, len(td.Fields))
	for i, f := range td.Fields {
		names[i] = f.Name
	}
	return names, nil
}

func literalOutputs(cells []model.Cell, want int, dec string, line int) ([]ir.Value, error) {
	if len(cells) != want {
		return nil, diag.Newf(diag.CodeArity, line, "décision %q: %d sorties, %d attendue(s)", dec, len(cells), want)
	}
	out := make([]ir.Value, len(cells))
	for i, c := range cells {
		v, err := literalValue(c.Node)
		if err != nil {
			return nil, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("décision %q: sortie %q", dec, c.Src), err).WithCol(c.Col)
		}
		out[i] = v
	}
	return out, nil
}

// normalizeCell transforme une cellule de condition en CellTest géométrique (ou Op=Prog).
func normalizeCell(c model.Cell, valid map[string]bool, bkms map[string]model.BKM, dec string) (ir.CellTest, error) {
	if c.Dash {
		return ir.CellTest{Op: ir.OpAny, Src: c.Src, Line: c.Line}, nil
	}
	ct, err := normalizeNode(c.Node, c.Src, valid, bkms, dec, c.Line, c.Col)
	if err != nil {
		return ir.CellTest{}, err
	}
	// Trace de justification : Src/Line de la cellule de plus haut niveau (pas les sous-tests).
	ct.Src = c.Src
	ct.Line = c.Line
	return ct, nil
}

func normalizeNode(node feel.Node, src string, valid map[string]bool, bkms map[string]model.BKM, dec string, line, col int) (ir.CellTest, error) {
	switch n := node.(type) {
	case *feel.RangeNode:
		lo, err := literalValue(n.Start)
		if err != nil {
			return ir.CellTest{}, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("borne basse de %q", src), err).WithCol(col)
		}
		hi, err := literalValue(n.End)
		if err != nil {
			return ir.CellTest{}, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("borne haute de %q", src), err).WithCol(col)
		}
		return ir.CellTest{Op: ir.OpInRange, A: lo, B: hi, AOpen: n.StartOpen, BOpen: n.EndOpen}, nil
	case *feel.MultiTests:
		ct := ir.CellTest{Op: ir.OpInSet}
		for _, el := range n.Elements {
			sub, err := normalizeNode(el, src, valid, bkms, dec, line, col)
			if err != nil {
				return ir.CellTest{}, err
			}
			ct.Sub = append(ct.Sub, sub)
		}
		return ct, nil
	case *feel.Binop:
		if v, ok := n.Left.(*feel.Var); ok && v.Name == "?" {
			if lit, err := literalValue(n.Right); err == nil {
				op, err := mapOp(n.Op, line, col)
				if err != nil {
					return ir.CellTest{}, err
				}
				return ir.CellTest{Op: op, A: lit}, nil
			}
			// "? op <expr non littérale>" (ex: "< monthly_debt") -> cellule Op=Prog
			return progCell(node, valid, bkms, dec, line, col)
		}
		return progCell(node, valid, bkms, dec, line, col)
	case *feel.NumberNode, *feel.StringNode, *feel.BoolNode:
		lit, err := literalValue(node)
		if err != nil {
			return ir.CellTest{}, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("cellule %q", src), err).WithCol(col)
		}
		return ir.CellTest{Op: ir.OpEq, A: lit}, nil
	case *feel.FunCall:
		// `not(<test>)` en cellule : négation GÉOMÉTRIQUE (reste analysable par le vérificateur).
		// not(x) -> test(x) nié ; not(a, b, ...) -> hors de l'ensemble {a, b, ...}.
		if v, ok := n.FunRef.(*feel.Var); ok && v.Name == "not" {
			return negateCell(n, src, valid, bkms, dec, line, col)
		}
		// Autre invocation (BKM, floor/round) utilisée comme test booléen -> cellule Op=Prog.
		return progCell(node, valid, bkms, dec, line, col)
	default:
		return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cellule %q: construct non supporté en v2", src).WithCol(col)
	}
}

// negateCell normalise `not(...)` en cellule : un test géométrique inversé (Negate), ou un
// OpNot appliqué au programme pour un test non géométrique (Op=Prog). Échec franc sur kwargs / 0 arg.
func negateCell(n *feel.FunCall, src string, valid map[string]bool, bkms map[string]model.BKM, dec string, line, col int) (ir.CellTest, error) {
	if len(n.Args) == 0 {
		return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cellule %q: `not()` attend au moins un test", src).WithCol(col)
	}
	for _, a := range n.Args {
		if a.Name != "" {
			return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cellule %q: `not(...)` n'accepte pas d'arguments nommés", src).WithCol(col)
		}
	}
	if len(n.Args) == 1 {
		inner, err := normalizeNode(n.Args[0].Arg, src, valid, bkms, dec, line, col)
		if err != nil {
			return ir.CellTest{}, err
		}
		if inner.Op == ir.OpProg {
			// non géométrique : négation à l'exécution (OpNot sur le résultat booléen).
			inner.Prog.Code = append(inner.Prog.Code, ir.Instr{Op: ir.OpNot})
			return inner, nil
		}
		inner.Negate = !inner.Negate // toggle (gère not(not(...)))
		return inner, nil
	}
	// not(a, b, ...) : hors de l'ensemble — OU des sous-tests, le tout nié.
	ct := ir.CellTest{Op: ir.OpInSet, Negate: true}
	for _, a := range n.Args {
		sub, err := normalizeNode(a.Arg, src, valid, bkms, dec, line, col)
		if err != nil {
			return ir.CellTest{}, err
		}
		if sub.Op == ir.OpProg {
			return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cellule %q: `not(...)` multi-tests exige des tests géométriques", src).WithCol(col)
		}
		ct.Sub = append(ct.Sub, sub)
	}
	return ct, nil
}

// progCell compile une cellule en expression booléenne (couche bytecode), `?` = valeur de colonne.
func progCell(node feel.Node, valid map[string]bool, bkms map[string]model.BKM, dec string, line, col int) (ir.CellTest, error) {
	prog, err := lowerExpr(node, bkms)
	if err != nil {
		return ir.CellTest{}, diag.Wrap(diag.CodeUnsupported, line, "cellule", err).WithCol(col)
	}
	if err := checkVars(prog.Vars, valid, dec, line); err != nil {
		return ir.CellTest{}, err
	}
	return ir.CellTest{Op: ir.OpProg, Prog: prog}, nil
}

func checkVars(vars []string, valid map[string]bool, dec string, line int) error {
	for _, v := range vars {
		if !valid[v] {
			return diag.Newf(diag.CodeUndeclared, line,
				"décision %q: référence %q, non déclarée (input ou décision)", dec, v).
				WithSuggestion("noms déclarés : " + strings.Join(sortedKeys(valid), ", "))
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
		return ir.Value{}, diag.Newf(diag.CodeLiteral, 0, "littéral attendu, obtenu %T", node)
	}
}

func mapOp(op string, line, col int) (ir.Op, error) {
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
		return 0, diag.Newf(diag.CodeUnsupported, line, "opérateur de cellule non supporté en v2: %q", op).WithCol(col)
	}
}

func parseHitPolicy(s string, no int) (ir.HitPolicy, ir.Aggregation, error) {
	switch s {
	case "":
		return 0, 0, diag.New(diag.CodeHitPolicy, no, "`hit:` manquant").
			WithSuggestion("ex : hit: first")
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
		return 0, 0, diag.Newf(diag.CodeHitPolicy, no, "hit policy non supportée: %q", s).
			WithSuggestion("politiques : first, unique, any, priority, rule order, collect[ sum|min|max|count]")
	}
}

// parseDomain interprète une contrainte de domaine d'entrée (`in [a..b]`, `>= 0`, `in {..}`)
// en ir.Domain pour la vérification de complétude. Une forme non reconnue -> DomNone (pas d'erreur).
func parseDomain(s string) (ir.Domain, error) {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return ir.Domain{Kind: ir.DomNone}, nil
	}
	if strings.HasPrefix(rest, "in ") {
		rest = strings.TrimSpace(rest[len("in "):])
	}
	// Enum : { v1, v2, ... }
	if strings.HasPrefix(rest, "{") {
		body := strings.TrimSuffix(strings.TrimPrefix(rest, "{"), "}")
		dom := ir.Domain{Kind: ir.DomEnum}
		for _, part := range strings.Split(body, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			node, err := feel.ParseString(part)
			if err != nil {
				return ir.Domain{}, diag.Wrap(diag.CodeFeelSyntax, 0, fmt.Sprintf("domaine enum %q", part), err)
			}
			v, err := literalValue(node)
			if err != nil {
				return ir.Domain{}, diag.Wrap(diag.CodeLiteral, 0, fmt.Sprintf("domaine enum %q", part), err)
			}
			dom.Enum = append(dom.Enum, v)
		}
		return dom, nil
	}
	node, err := feel.ParseString(rest)
	if err != nil {
		return ir.Domain{Kind: ir.DomNone}, nil // non interprétable -> pas de domaine (dégradation)
	}
	switch n := node.(type) {
	case *feel.RangeNode:
		lo, err := literalValue(n.Start)
		if err != nil {
			return ir.Domain{Kind: ir.DomNone}, nil
		}
		hi, err := literalValue(n.End)
		if err != nil {
			return ir.Domain{Kind: ir.DomNone}, nil
		}
		return ir.Domain{Kind: ir.DomNumeric, Lo: lo, Hi: hi, LoOpen: n.StartOpen, HiOpen: n.EndOpen}, nil
	case *feel.Binop:
		if v, ok := n.Left.(*feel.Var); ok && v.Name == "?" {
			lit, err := literalValue(n.Right)
			if err != nil {
				return ir.Domain{Kind: ir.DomNone}, nil
			}
			switch n.Op {
			case ">=":
				return ir.Domain{Kind: ir.DomNumeric, Lo: lit, HiInf: true}, nil
			case ">":
				return ir.Domain{Kind: ir.DomNumeric, Lo: lit, LoOpen: true, HiInf: true}, nil
			case "<=":
				return ir.Domain{Kind: ir.DomNumeric, Hi: lit, LoInf: true}, nil
			case "<":
				return ir.Domain{Kind: ir.DomNumeric, Hi: lit, HiOpen: true, LoInf: true}, nil
			}
		}
	}
	return ir.Domain{Kind: ir.DomNone}, nil
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

// sortedKeys renvoie les clés vraies d'un set, triées (pour des suggestions déterministes).
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k, ok := range set {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

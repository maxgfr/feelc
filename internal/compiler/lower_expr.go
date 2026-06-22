package compiler

import (
	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/model"
)

// lowerExpr compile un nœud FEEL en ExprProgram (bytecode plat).
// Sous-ensemble v2 : littéraux, variables (dont `?`), arithmétique +-*/, comparaisons, and/or,
// et l'INVOCATION de BKM `name(a, b)` (inlinée par substitution AST des paramètres — zéro
// nouvel opcode, la VM ne sait pas qu'un BKM a existé). Les autres constructs (if/then/else,
// for/some/every, **) échouent franchement.
// Garde-fous d'inlining (bornent la RAM de compilation face à une source pathologique :
// récursion mutuelle, ou expansion exponentielle de BKM imbriqués acycliques). Symétrique au
// gridBudget de la vérification — jamais conformer en silence : on échoue franchement.
const (
	maxInlineDepth = 256     // profondeur max de la chaîne d'inlining
	maxInstrBudget = 200_000 // nb max d'instructions bytecode émises pour une expression
)

func lowerExpr(node feel.Node, bkms map[string]model.BKM) (*ir.ExprProgram, error) {
	l := &lowerer{prog: &ir.ExprProgram{}, varIdx: map[string]int{}, bkms: bkms, recursive: recursiveBKMs(bkms)}
	if err := l.emit(node); err != nil {
		return nil, err
	}
	l.prog.MaxStack = maxStack(l.prog.Code)
	return l.prog, nil
}

type lowerer struct {
	prog        *ir.ExprProgram
	varIdx      map[string]int
	bkms        map[string]model.BKM // BKM connus (invocations inlinables)
	recursive   map[string]bool      // BKM sur un cycle (auto/mutuel) — invocation interdite
	inlineDepth int                  // profondeur d'inlining courante (garde-fou anti-explosion)
}

// recursiveBKMs calcule statiquement l'ensemble des BKM qui s'invoquent eux-mêmes,
// directement ou via un cycle (mutuel) — détecté par fermeture transitive du graphe d'appels.
// Distinguer ce cycle STATIQUE de l'imbrication `f(f(x))` (légitime, acyclique) est crucial :
// une garde par pile d'inlining confondrait les deux (un appel en position d'argument n'est PAS
// un cycle). Les BKM récursifs sont rejetés à l'invocation ; le graphe résiduel est acyclique
// donc l'inlining termine.
func recursiveBKMs(bkms map[string]model.BKM) map[string]bool {
	calls := make(map[string]map[string]bool, len(bkms))
	for name, b := range bkms {
		set := map[string]bool{}
		collectBKMCalls(b.Body.Node, bkms, set)
		calls[name] = set
	}
	rec := map[string]bool{}
	for name := range bkms {
		// name est récursif s'il s'atteint lui-même en ≥1 arête.
		seen := map[string]bool{}
		var reaches func(cur string) bool
		reaches = func(cur string) bool {
			for callee := range calls[cur] {
				if callee == name {
					return true
				}
				if !seen[callee] {
					seen[callee] = true
					if reaches(callee) {
						return true
					}
				}
			}
			return false
		}
		if reaches(name) {
			rec[name] = true
		}
	}
	return rec
}

// collectBKMCalls collecte les noms de BKM invoqués dans une expression (sous-ensemble lowerable).
func collectBKMCalls(node feel.Node, bkms map[string]model.BKM, out map[string]bool) {
	switch n := node.(type) {
	case *feel.FunCall:
		if v, ok := n.FunRef.(*feel.Var); ok {
			if _, isBKM := bkms[v.Name]; isBKM {
				out[v.Name] = true
			}
		}
		for _, a := range n.Args {
			collectBKMCalls(a.Arg, bkms, out)
		}
	case *feel.Binop:
		collectBKMCalls(n.Left, bkms, out)
		collectBKMCalls(n.Right, bkms, out)
	}
}

func (l *lowerer) constIdx(v ir.Value) uint32 {
	idx := uint32(len(l.prog.Consts))
	l.prog.Consts = append(l.prog.Consts, v)
	return idx
}

func (l *lowerer) varIndex(name string) uint32 {
	if i, ok := l.varIdx[name]; ok {
		return uint32(i)
	}
	i := len(l.prog.Vars)
	l.prog.Vars = append(l.prog.Vars, name)
	l.varIdx[name] = i
	return uint32(i)
}

func (l *lowerer) push(op ir.Opcode, arg uint32) {
	l.prog.Code = append(l.prog.Code, ir.Instr{Op: op, Arg: arg})
}

func (l *lowerer) emit(node feel.Node) error {
	if len(l.prog.Code) > maxInstrBudget {
		return diag.Newf(diag.CodeUnsupported, 0,
			"expression trop volumineuse à compiler (> %d instructions) — inlining BKM excessif", maxInstrBudget)
	}
	switch n := node.(type) {
	case *feel.NumberNode:
		d, err := decimal.Parse(n.Value)
		if err != nil {
			return err
		}
		l.push(ir.OpPushConst, l.constIdx(ir.Num(d)))
	case *feel.StringNode:
		l.push(ir.OpPushConst, l.constIdx(ir.Str(n.Content())))
	case *feel.BoolNode:
		l.push(ir.OpPushConst, l.constIdx(ir.Bool(n.Value)))
	case *feel.Var:
		if n.Name == "?" {
			l.push(ir.OpLoadInput, 0)
		} else {
			l.push(ir.OpLoadVar, l.varIndex(n.Name))
		}
	case *feel.Binop:
		if err := l.emit(n.Left); err != nil {
			return err
		}
		if err := l.emit(n.Right); err != nil {
			return err
		}
		op, err := binopcode(n.Op)
		if err != nil {
			return err
		}
		l.push(op, 0)
	case *feel.FunCall:
		return l.emitBKMCall(n)
	default:
		return diag.Newf(diag.CodeUnsupported, 0, "expression non supportée en v2: %T", node)
	}
	return nil
}

// emitBKMCall inline une invocation de BKM `name(a1, ..., an)` : substitution AST des
// paramètres par les nœuds d'arguments, puis lowering normal du corps substitué.
func (l *lowerer) emitBKMCall(fc *feel.FunCall) error {
	ref, ok := fc.FunRef.(*feel.Var)
	if !ok {
		return diag.Newf(diag.CodeUnsupported, 0, "invocation: seul `nom(...)` est supporté, obtenu %s", fc.FunRef.Repr())
	}
	name := ref.Name
	bkm, ok := l.bkms[name]
	if !ok {
		return diag.Newf(diag.CodeUnsupported, 0, "invocation %q: BKM inconnu", name).
			WithSuggestion("déclarez `bkm " + name + "(...)` ou vérifiez le nom")
	}
	// Récursion (auto ou mutuelle) détectée STATIQUEMENT : refus franc. L'imbrication acyclique
	// `f(f(x))` n'est PAS récursive et reste autorisée.
	if l.recursive[name] {
		return diag.Newf(diag.CodeUnsupported, 0,
			"récursion BKM interdite: %q s'invoque lui-même (directement ou en cycle)", name)
	}
	if len(fc.Args) != len(bkm.Params) {
		return diag.Newf(diag.CodeArity, 0, "invocation %q: %d argument(s) pour %d paramètre(s)",
			name, len(fc.Args), len(bkm.Params))
	}
	// Périmètre v1 : positionnel uniquement (pas de kwargs `f(x: 1)`).
	subst := make(map[string]feel.Node, len(bkm.Params))
	for i, arg := range fc.Args {
		if arg.Name != "" {
			return diag.Newf(diag.CodeUnsupported, 0,
				"invocation %q: arguments nommés non supportés (BKM positionnel uniquement)", name)
		}
		subst[bkm.Params[i].Name] = arg.Arg
	}
	// `?` (valeur de colonne) n'a pas de sens dans un corps de BKM : refus franc.
	if hasColumnRef(bkm.Body.Node) {
		return diag.Newf(diag.CodeUnsupported, 0, "BKM %q: `?` (valeur de colonne) interdit dans un corps de BKM", name)
	}
	// Garde-fou de profondeur (borne la RAM même pour une imbrication acyclique pathologique).
	l.inlineDepth++
	if l.inlineDepth > maxInlineDepth {
		l.inlineDepth--
		return diag.Newf(diag.CodeUnsupported, 0,
			"profondeur d'inlining BKM excessive (> %d) sur %q — imbrication trop profonde", maxInlineDepth, name)
	}
	body := substitute(bkm.Body.Node, subst)
	err := l.emit(body)
	l.inlineDepth--
	return err
}

// substitute clone un nœud FEEL en remplaçant chaque *feel.Var{Name ∈ subst} par le nœud
// d'argument correspondant. Couvre le sous-ensemble lowerable ; les autres types sont rendus
// tels quels (le lowering les rejettera franchement si non supportés).
func substitute(node feel.Node, subst map[string]feel.Node) feel.Node {
	switch n := node.(type) {
	case *feel.Var:
		if repl, ok := subst[n.Name]; ok {
			return repl
		}
		return n
	case *feel.Binop:
		return &feel.Binop{Op: n.Op, Left: substitute(n.Left, subst), Right: substitute(n.Right, subst)}
	case *feel.FunCall:
		args := make([]feel.FunCallArg, len(n.Args))
		for i, a := range n.Args {
			args[i] = feel.FunCallArg{Name: a.Name, Arg: substitute(a.Arg, subst)}
		}
		return &feel.FunCall{FunRef: n.FunRef, Args: args}
	default:
		return node // littéraux et types non supportés : inchangés
	}
}

// hasColumnRef indique si l'expression référence `?` (valeur de colonne) dans le sous-ensemble
// lowerable. Sert à interdire `?` dans un corps de BKM (les args, eux, peuvent contenir `?`).
func hasColumnRef(node feel.Node) bool {
	switch n := node.(type) {
	case *feel.Var:
		return n.Name == "?"
	case *feel.Binop:
		return hasColumnRef(n.Left) || hasColumnRef(n.Right)
	case *feel.FunCall:
		for _, a := range n.Args {
			if hasColumnRef(a.Arg) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func binopcode(op string) (ir.Opcode, error) {
	switch op {
	case "+":
		return ir.OpAdd, nil
	case "-":
		return ir.OpSub, nil
	case "*":
		return ir.OpMul, nil
	case "/":
		return ir.OpDivOp, nil
	case "=":
		return ir.OpEqOp, nil
	case "!=":
		return ir.OpNeOp, nil
	case "<":
		return ir.OpLtOp, nil
	case "<=":
		return ir.OpLeOp, nil
	case ">":
		return ir.OpGtOp, nil
	case ">=":
		return ir.OpGeOp, nil
	case "and":
		return ir.OpAnd, nil
	case "or":
		return ir.OpOr, nil
	default:
		return 0, diag.Newf(diag.CodeUnsupported, 0, "opérateur non supporté en v2: %q", op)
	}
}

// maxStack calcule la profondeur de pile maximale (T2 : pas de sauts).
func maxStack(code []ir.Instr) int {
	depth, max := 0, 0
	for _, in := range code {
		switch in.Op {
		case ir.OpPushConst, ir.OpLoadVar, ir.OpLoadInput:
			depth++
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDivOp,
			ir.OpEqOp, ir.OpNeOp, ir.OpLtOp, ir.OpLeOp, ir.OpGtOp, ir.OpGeOp,
			ir.OpAnd, ir.OpOr:
			depth-- // pop 2, push 1
		}
		if depth > max {
			max = depth
		}
	}
	return max
}

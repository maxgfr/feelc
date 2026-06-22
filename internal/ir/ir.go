// Package ir définit la représentation intermédiaire de feelc : un modèle compilé,
// immuable après compilation, que la VM exécute et que la vérification analyse.
//
// Conception cible (cf. plan) : 3 couches superposées —
//   (1) graphe de décisions topo-triées ;
//   (2) tables déclaratives normalisées en CellTest (forme géométrique = analysable) ;
//   (3) bytecode FEEL plat par cellule/expression (Tranche 2+).
// La Tranche 1 ne matérialise que (1) + (2) avec des sorties littérales.
package ir

// Op : opérateur normalisé d'une cellule de table (unary test).
type Op uint8

const (
	OpAny     Op = iota // "-" : toujours vrai (don't care)
	OpEq                // littéral nu : égalité
	OpNe                // "!= x"
	OpLt                // "< x"
	OpLe                // "<= x"
	OpGt                // "> x"
	OpGe                // ">= x"
	OpInRange           // "[a..b)" etc. (Tranche 2)
	OpInSet             // "a","b"   (Tranche 3)
	OpProg              // expression libre -> bytecode (Tranche 2)
)

// CellTest : forme normalisée d'une cellule d'entrée. C'est exactement la géométrie
// (comparaison/intervalle/ensemble) que le vérificateur sait décomposer.
type CellTest struct {
	Op    Op
	A     Value        // comparand (Eq/Ne/Lt..) ou borne basse (InRange)
	B     Value        // borne haute (InRange)
	AOpen bool         // borne basse exclue ?
	BOpen bool         // borne haute exclue ?
	Sub   []CellTest   // OpInSet : OU de sous-tests (sémantique virgule des unary tests DMN)
	Prog  *ExprProgram // OpProg : cellule = expression libre (référence une autre colonne, arithmétique)
}

// HitPolicy : politique de résolution d'une table de décision (sémantique DMN).
type HitPolicy uint8

const (
	HitUnique HitPolicy = iota
	HitAny
	HitFirst
	HitPriority
	HitCollect
	HitRuleOrder
)

// Rule : une ligne de table. Conds aligné sur les colonnes d'entrée, Outputs sur les sorties.
type Rule struct {
	Conds   []CellTest
	Outputs []Value // littéraux en Tranche 1 (expressions en Tranche 2)
}

// Aggregation : fonction d'agrégation d'une hit policy COLLECT.
type Aggregation uint8

const (
	AggNone  Aggregation = iota // COLLECT brut -> liste des sorties
	AggSum                      // C+
	AggMin                      // C<
	AggMax                      // C>
	AggCount                    // C#
)

// DecisionTable : la logique d'une décision sous forme de table.
type DecisionTable struct {
	Inputs    []string // noms des variables d'entrée, dans l'ordre des colonnes
	Outputs   []string // noms des sorties (1 = sortie scalaire ; >1 = context)
	Rules     []Rule
	HitPolicy HitPolicy
	Agg       Aggregation // si HitCollect
	Priority  []Value     // si HitPriority : valeurs de sortie, ordre décroissant de priorité
	Default   []Value     // sortie de la ligne `default` (nil si absente)
}

// DecisionKind distingue table et expression littérale.
type DecisionKind uint8

const (
	KindTable DecisionKind = iota
	KindLiteralExpr // Tranche 2
)

// Decision : un nœud du graphe de décisions (DRG).
type Decision struct {
	Name  string
	Kind  DecisionKind
	Table *DecisionTable // si KindTable
	Expr  *ExprProgram   // si KindLiteralExpr
	Deps  []string       // dépendances (information requirements)
}

// Type : type statique d'une variable.
type Type uint8

const (
	TypeNumber Type = iota
	TypeString
	TypeBool
	TypeContext // Tranche 2
)

// CompiledModel : le modèle prêt à exécuter (sortie du compilateur).
type CompiledModel struct {
	Name      string
	Inputs    map[string]Type
	Decisions []Decision

	byName map[string]int
}

// Decision retrouve une décision par nom (index paresseux).
func (cm *CompiledModel) Decision(name string) (*Decision, bool) {
	if cm.byName == nil {
		cm.byName = make(map[string]int, len(cm.Decisions))
		for i := range cm.Decisions {
			cm.byName[cm.Decisions[i].Name] = i
		}
	}
	if i, ok := cm.byName[name]; ok {
		return &cm.Decisions[i], true
	}
	return nil, false
}

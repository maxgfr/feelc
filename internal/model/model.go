// Package model est la représentation conceptuelle d'un modèle de règles, telle que
// produite par le parseur DSL et consommée par le compilateur. Elle conserve l'AST FEEL
// des cellules et leurs positions source (pont NL<->règle + explication).
package model

import feel "github.com/pbinitiative/feel"

// Type : type scalaire déclaré d'une variable (forme source).
type Type string

const (
	TypeNumber Type = "number"
	TypeString Type = "string"
	TypeBool   Type = "boolean"
)

// Input : une donnée d'entrée déclarée (Input Data DMN).
type Input struct {
	Name   string
	Type   Type
	Domain string // contrainte de domaine brute (ex: "in [300..850]", ">= 0") ; exploitée par la vérif (T4)
	Line   int
}

// Field : un champ d'un type context (sortie multi-colonnes).
type Field struct {
	Name string
	Type Type
}

// TypeDecl : déclaration `type Name = context { f1: t1, f2: t2 }`.
type TypeDecl struct {
	Name   string
	Fields []Field
	Line   int
}

// Cell : une cellule de table (condition d'entrée ou sortie), avec sa trace source.
type Cell struct {
	Src  string    // texte source brut (pour l'explication)
	Dash bool      // "-" : any/don't-care (jamais passé au parseur FEEL)
	Node feel.Node // AST FEEL (nil si Dash)
	Line int
	Col  int
}

// Rule : une ligne de table (conditions => sorties), ou la ligne `default`.
type Rule struct {
	Conds     []Cell
	Outputs   []Cell
	IsDefault bool // ligne `default` : s'applique quand aucune règle ne matche
	Line      int
}

// Decision : une décision du DRG. Soit une table (Rules), soit une expression (Expr).
type Decision struct {
	Name      string
	TypeName  string // "number"/"string"/"boolean" ou un nom de type context déclaré
	Needs     []string
	HitPolicy string
	Rules     []Rule
	Expr      *Cell // si != nil : décision literal-expression (TypeName scalaire), pas de table
	Line      int
}

// Model : un modèle complet.
type Model struct {
	Name      string
	Inputs    []Input
	Types     []TypeDecl
	Decisions []Decision
}

// Type retrouve une déclaration de type context par nom.
func (m *Model) Type(name string) (TypeDecl, bool) {
	for _, t := range m.Types {
		if t.Name == name {
			return t, true
		}
	}
	return TypeDecl{}, false
}

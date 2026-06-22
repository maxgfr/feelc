// Package model est la représentation conceptuelle d'un modèle de règles, telle que
// produite par le parseur DSL et consommée par le compilateur. Elle conserve l'AST FEEL
// des cellules et leurs positions source (pont NL<->règle + explication).
package model

import feel "github.com/pbinitiative/feel"

// Type : type déclaré d'une variable (forme source).
type Type string

const (
	TypeNumber Type = "number"
	TypeString Type = "string"
	TypeBool   Type = "boolean"
)

// Input : une donnée d'entrée déclarée (Input Data DMN).
type Input struct {
	Name string
	Type Type
	Line int
}

// Cell : une cellule de table (condition d'entrée ou sortie), avec sa trace source.
type Cell struct {
	Src  string    // texte source brut (pour l'explication)
	Dash bool      // "-" : any/don't-care (jamais passé au parseur FEEL)
	Node feel.Node // AST FEEL (nil si Dash)
	Line int
	Col  int
}

// Rule : une ligne de table (conditions => sorties).
type Rule struct {
	Conds   []Cell
	Outputs []Cell
	Line    int
}

// Decision : une décision du DRG (table en Tranche 1).
type Decision struct {
	Name      string
	Type      Type
	Needs     []string
	HitPolicy string
	Rules     []Rule
	Line      int
}

// Model : un modèle complet.
type Model struct {
	Name      string
	Inputs    []Input
	Decisions []Decision
}

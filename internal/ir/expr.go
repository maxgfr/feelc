package ir

// Opcode : jeu d'instructions de la VM d'expressions FEEL (couche 3 de l'IR).
// Bytecode plat -> jamais de tree-walking au runtime (cf. plan). Étendu au fil des tranches.
type Opcode uint8

const (
	OpPushConst Opcode = iota // empile Consts[Arg]
	OpLoadVar                 // empile la valeur de Vars[Arg] (input ou décision amont)
	OpLoadInput               // empile la valeur de colonne courante '?' (cellules Op=Prog)
	OpAdd
	OpSub
	OpMul
	OpDivOp // division décimale exacte
	OpNeg   // négation arithmétique unaire
	OpEqOp
	OpNeOp
	OpLtOp
	OpLeOp
	OpGtOp
	OpGeOp
	OpAnd
	OpOr
	OpNot
	OpJmpFalse // saut conditionnel (if) : dépile un booléen, saute à Arg si faux
	OpJmp      // saut inconditionnel à Arg
)

// Instr : une instruction (opcode + argument entier dense).
type Instr struct {
	Op  Opcode
	Arg uint32
}

// ExprProgram : programme bytecode d'une expression FEEL (cellule Op=Prog ou décision literal-expr).
type ExprProgram struct {
	Code     []Instr
	Consts   []Value
	Vars     []string // noms référencés ; l'Arg de OpLoadVar indexe ici
	MaxStack int
}

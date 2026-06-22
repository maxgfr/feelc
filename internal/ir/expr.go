package ir

// Opcode: instruction set of the FEEL expression VM (layer 3 of the IR).
// Flat bytecode -> never tree-walking at runtime (cf. plan). Extended as slices are added.
type Opcode uint8

const (
	OpPushConst Opcode = iota // push Consts[Arg]
	OpLoadVar                 // push the value of Vars[Arg] (input or upstream decision)
	OpLoadInput               // push the value of the current column '?' (Op=Prog cells)
	OpAdd
	OpSub
	OpMul
	OpDivOp // exact decimal division
	OpNeg   // unary arithmetic negation
	OpEqOp
	OpNeOp
	OpLtOp
	OpLeOp
	OpGtOp
	OpGeOp
	OpAnd
	OpOr
	OpNot
	OpJmpFalse // conditional jump (if): pops a boolean, jumps to Arg if false
	OpJmp      // unconditional jump to Arg
	OpFloor    // round toward -∞ (built-in floor, single-arg)
	OpCeil     // round toward +∞ (built-in ceiling, single-arg)
	OpRound    // round to the nearest integer, HALF_EVEN (built-in round, single-arg)
)

// Instr: an instruction (opcode + dense integer argument).
type Instr struct {
	Op  Opcode
	Arg uint32
}

// ExprProgram: bytecode program of a FEEL expression (Op=Prog cell or literal-expr decision).
type ExprProgram struct {
	Code     []Instr
	Consts   []Value
	Vars     []string // referenced names; the Arg of OpLoadVar indexes here
	MaxStack int
}

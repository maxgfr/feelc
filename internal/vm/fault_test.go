package vm

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/ir"
)

// A malformed bytecode program (here: OpAdd popping an empty stack) must return a clean engine error,
// not crash the host with an index-out-of-range panic. Defensive guard for corrupt/tampered .ir.bin.
func TestEvalExpr_StackUnderflowReturnsError(t *testing.T) {
	e := &evaluator{cm: &ir.CompiledModel{}, inputs: map[string]ir.Value{}, memo: map[string]ir.Value{}, state: map[string]int{}}
	bad := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpAdd}}, MaxStack: 0} // OpAdd pops 2 from an empty stack
	_, err := e.evalExpr(bad, nil)
	if err == nil {
		t.Fatal("expected an error from a stack-underflowing program, got nil")
	}
	if !strings.Contains(err.Error(), "malformed expression program") {
		t.Errorf("error = %q, want it to mention a malformed expression program", err)
	}
}

//go:build js && wasm

// Command feelc-wasm exposes the deterministic feelc engine to the browser as a global `feelc`
// object whose functions mirror the HTTP service (verify/run/graph/trace/model/required/check).
// Everything runs client-side: the playground compiles, verifies and evaluates `.rules` with the
// REAL engine — byte-for-byte identical to the CLI, no backend, no LLM. AI authoring stays in
// `feelc serve --ui`. Each function takes/returns JSON strings, recovers from panics, and returns
// {"error": ...} (structured diag.Error on a compile failure).
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall/js"

	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/graph"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/modelinfo"
	"github.com/maxgfr/feelc/internal/trace"
)

func main() {
	feelc := js.Global().Get("Object").New()
	// Source-based functions (mirror the HTTP service). Unchanged: the static playground uses them.
	feelc.Set("verify", wrap(verifyFn))
	feelc.Set("run", wrap(runFn))
	feelc.Set("graph", wrap(graphFn))
	feelc.Set("trace", wrap(traceFn))
	feelc.Set("model", wrap(modelFn))
	feelc.Set("required", wrap(requiredFn))
	feelc.Set("check", wrap(checkFn))
	// Compiled-model handle path: compile (or load a precompiled .ir.bin) ONCE, evaluate MANY times
	// without recompiling on every call (the reactive "ultra-opti" case). Consumed by feelc.
	feelc.Set("compile", wrap(compileFn))
	feelc.Set("load", wrap(loadFn))
	feelc.Set("export", wrap(exportFn))
	feelc.Set("evalCompiled", wrap(evalCompiledFn))
	feelc.Set("evalCompiledBatch", wrap(evalCompiledBatchFn))
	feelc.Set("infoCompiled", wrap(infoCompiledFn))
	feelc.Set("requiredCompiled", wrap(requiredCompiledFn))
	feelc.Set("dispose", wrap(disposeFn))
	feelc.Set("ready", js.ValueOf(true))
	// Expose under a caller-chosen global name so multiple instances per realm don't collide;
	// defaults to `feelc`, which the static playground relies on.
	token := "feelc"
	if t := js.Global().Get("feelcInstanceToken"); t.Type() == js.TypeString && t.String() != "" {
		token = t.String()
	}
	js.Global().Set(token, feelc)
	select {} // keep the Go runtime alive for the registered callbacks
}

// wrap adapts an engine function to a JS callback: it returns a JSON string on success, or
// {"error": ...} on failure (structured diag.Error for compile errors), and never lets a panic
// escape into JS.
func wrap(fn func(args []js.Value) (any, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if r := recover(); r != nil {
				result = marshal(map[string]any{"error": fmt.Sprintf("panic: %v", r)})
			}
		}()
		out, err := fn(args)
		if err != nil {
			var de *diag.Error
			if errors.As(err, &de) {
				return marshal(map[string]any{"error": de})
			}
			return marshal(map[string]any{"error": err.Error()})
		}
		return marshal(out)
	})
}

func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"internal: marshal failed"}`
	}
	return string(b)
}

func argStr(args []js.Value, i int) string {
	if i < len(args) {
		return args[i].String()
	}
	return ""
}

// verifyFn: source -> {hash, report, blockers}. Mirrors POST /v1/verify.
func verifyFn(args []js.Value) (any, error) {
	_, hash, rep, err := loader.Compile([]byte(argStr(args, 0)))
	if err != nil {
		return nil, err
	}
	return map[string]any{"hash": hash, "report": rep, "blockers": rep.Blockers()}, nil
}

// runFn: {rules,decision,input,explain,full} -> {decision,output,trace?}. Mirrors POST /v1/run.
func runFn(args []js.Value) (any, error) {
	var doc struct {
		Rules    string         `json:"rules"`
		Decision string         `json:"decision"`
		Input    map[string]any `json:"input"`
		Explain  bool           `json:"explain"`
		Full     bool           `json:"full"`
	}
	dec := json.NewDecoder(strings.NewReader(argStr(args, 0)))
	dec.UseNumber() // decimal exactness of input numbers (mirror the HTTP handler)
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Decision == "" {
		return nil, errors.New("`decision` field required")
	}
	cm, _, _, err := loader.Compile([]byte(doc.Rules))
	if err != nil {
		return nil, err
	}
	return evalResult(cm, doc.Decision, doc.Input, doc.Explain, doc.Full)
}

// evalResult evaluates a decision on an already-compiled model and assembles the
// {decision, output, trace?} response (decimals as fixed-notation numbers via JSONify). Shared by
// runFn (compile-then-eval) and evalCompiledFn (handle path), so the two never drift.
func evalResult(cm *ir.CompiledModel, decision string, input map[string]any, explainTrace, full bool) (any, error) {
	out, err := engine.Eval(cm, decision, input)
	if err != nil {
		return nil, err
	}
	resp := map[string]any{"decision": decision, "output": modelinfo.JSONify(out)}
	switch {
	case full:
		if ft, e := explain.ExplainFull(cm, decision, input); e == nil {
			resp["trace"] = explain.NormalizeFullJSON(ft)
		} else {
			resp["traceError"] = e.Error() // never silently drop the trace; `output` is already returned
		}
	case explainTrace:
		if tr, e := explain.Explain(cm, decision, input); e == nil {
			resp["trace"] = explain.NormalizeJSON(tr)
		} else {
			resp["traceError"] = e.Error()
		}
	}
	return resp, nil
}

// graphFn: source -> {graph,mermaid,dot,findings,blockers}. Mirrors POST /v1/graph.
func graphFn(args []js.Value) (any, error) {
	cm, _, rep, err := loader.Compile([]byte(argStr(args, 0)))
	if err != nil {
		return nil, err
	}
	g := graph.Build(cm, rep)
	return map[string]any{
		"mermaid": g.Mermaid(), "dot": g.DOT(), "graph": g,
		"findings": rep.Findings, "blockers": rep.Blockers(),
	}, nil
}

// traceFn: {rules,spec} -> trace.Report. Mirrors POST /v1/trace.
func traceFn(args []js.Value) (any, error) {
	var doc struct {
		Rules string `json:"rules"`
		Spec  string `json:"spec"`
	}
	if err := json.Unmarshal([]byte(argStr(args, 0)), &doc); err != nil {
		return nil, err
	}
	cm, _, _, err := loader.Compile([]byte(doc.Rules))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(doc.Spec) != "" {
		return trace.BuildWithSource(cm, []byte(doc.Spec)), nil
	}
	return trace.Build(cm), nil
}

// modelFn: source -> {name,inputs,decisions}. Mirrors GET /v1/model.
func modelFn(args []js.Value) (any, error) {
	cm, _, _, err := loader.Compile([]byte(argStr(args, 0)))
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": cm.Name, "inputs": modelinfo.Inputs(cm), "decisions": modelinfo.Decisions(cm)}, nil
}

// requiredFn: {rules,decision} -> {decision,inputs}. Mirrors POST /v1/required (question-flow).
func requiredFn(args []js.Value) (any, error) {
	var doc struct {
		Rules    string `json:"rules"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(argStr(args, 0)), &doc); err != nil {
		return nil, err
	}
	cm, _, _, err := loader.Compile([]byte(doc.Rules))
	if err != nil {
		return nil, err
	}
	return requiredFor(cm, doc.Decision)
}

// requiredFor resolves the inputs a decision transitively needs (question-flow), with metadata.
// Shared by requiredFn (source) and requiredCompiledFn (handle path).
func requiredFor(cm *ir.CompiledModel, decision string) (any, error) {
	req, err := cm.RequiredInputs(decision)
	if err != nil {
		return nil, err
	}
	byName := map[string]modelinfo.InputInfo{}
	for _, ii := range modelinfo.Inputs(cm) {
		byName[ii.Name] = ii
	}
	out := make([]modelinfo.InputInfo, 0, len(req))
	for _, n := range req {
		out = append(out, byName[n])
	}
	return map[string]any{"decision": decision, "inputs": out}, nil
}

// checkFn: {rules,claims} -> {report,blockers}. Mirrors POST /v1/check.
func checkFn(args []js.Value) (any, error) {
	var doc struct {
		Rules  string        `json:"rules"`
		Claims []check.Claim `json:"claims"`
	}
	dec := json.NewDecoder(strings.NewReader(argStr(args, 0)))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	cm, _, _, err := loader.Compile([]byte(doc.Rules))
	if err != nil {
		return nil, err
	}
	rep := check.Check(cm, doc.Claims)
	return map[string]any{"report": rep, "blockers": rep.Blockers()}, nil
}

// --- compiled-model handle registry ---
//
// The handle path lets the JS side compile (or load a precompiled .ir.bin) ONCE and evaluate MANY
// times without re-parsing/-compiling on each call (the reactive "ultra-opti" case). WASM is
// single-threaded, so a plain map needs no locking. Handles are freed explicitly via dispose().
var (
	models     = map[int]*ir.CompiledModel{}
	nextHandle = 1
)

func putModel(cm *ir.CompiledModel) int {
	h := nextHandle
	nextHandle++
	models[h] = cm
	return h
}

func getModel(handle int) (*ir.CompiledModel, error) {
	cm, ok := models[handle]
	if !ok {
		return nil, fmt.Errorf("unknown model handle %d (compile/load first, or it was disposed)", handle)
	}
	return cm, nil
}

// handleArg parses {"handle": n} from arg 0 (shared by export/info/dispose).
func handleArg(args []js.Value) (int, error) {
	var doc struct {
		Handle int `json:"handle"`
	}
	if err := json.Unmarshal([]byte(argStr(args, 0)), &doc); err != nil {
		return 0, err
	}
	return doc.Handle, nil
}

// compileFn: source -> {handle,hash,report,blockers}. Compiles once and retains the model for
// repeated evalCompiled calls (free it with dispose).
func compileFn(args []js.Value) (any, error) {
	cm, hash, rep, err := loader.Compile([]byte(argStr(args, 0)))
	if err != nil {
		return nil, err
	}
	return map[string]any{"handle": putModel(cm), "hash": hash, "report": rep, "blockers": rep.Blockers()}, nil
}

// loadFn: {ir} (base64 .ir.bin) -> {handle,hash}. Loads a precompiled artifact — the SAME bytes the
// native `feelc compile` emits — with no recompilation.
func loadFn(args []js.Value) (any, error) {
	var doc struct {
		IR string `json:"ir"`
	}
	if err := json.Unmarshal([]byte(argStr(args, 0)), &doc); err != nil {
		return nil, err
	}
	blob, err := base64.StdEncoding.DecodeString(doc.IR)
	if err != nil {
		return nil, fmt.Errorf("ir: invalid base64: %w", err)
	}
	cm, err := ir.Decode(blob)
	if err != nil {
		return nil, err
	}
	h, err := ir.Hash(cm)
	if err != nil {
		return nil, err
	}
	return map[string]any{"handle": putModel(cm), "hash": hex.EncodeToString(h[:])}, nil
}

// exportFn: {handle} -> {ir,hash}. Serializes a compiled model to a base64 .ir.bin for shipping
// (canonical bytes, identical to `feelc compile`).
func exportFn(args []js.Value) (any, error) {
	h, err := handleArg(args)
	if err != nil {
		return nil, err
	}
	cm, err := getModel(h)
	if err != nil {
		return nil, err
	}
	blob, err := ir.Encode(cm)
	if err != nil {
		return nil, err
	}
	sum, err := ir.Hash(cm)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ir": base64.StdEncoding.EncodeToString(blob), "hash": hex.EncodeToString(sum[:])}, nil
}

// evalCompiledFn: {handle,decision,input,explain,full} -> {decision,output,trace?}. The handle twin
// of runFn — evaluates a previously compiled/loaded model with no recompilation.
func evalCompiledFn(args []js.Value) (any, error) {
	var doc struct {
		Handle   int            `json:"handle"`
		Decision string         `json:"decision"`
		Input    map[string]any `json:"input"`
		Explain  bool           `json:"explain"`
		Full     bool           `json:"full"`
	}
	dec := json.NewDecoder(strings.NewReader(argStr(args, 0)))
	dec.UseNumber() // decimal exactness of input numbers (mirror runFn)
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Decision == "" {
		return nil, errors.New("`decision` field required")
	}
	cm, err := getModel(doc.Handle)
	if err != nil {
		return nil, err
	}
	return evalResult(cm, doc.Decision, doc.Input, doc.Explain, doc.Full)
}

// evalCompiledBatchFn: {handle,decision,inputs:[...],explain?,full?} -> {decision,results:[...]}.
// Amortizes the JS<->WASM boundary + JSON marshalling across N input rows (ONE parse, ONE handle
// lookup, ONE crossing) — the dominant cost of the per-call path (~14.7µs/op is mostly marshalling,
// not evaluation). The per-row engine evaluation is byte-identical to evalCompiledFn; a fresh
// evaluator per row (engine.Eval) means no memo/state is shared across rows. One bad row yields a
// {error} entry instead of failing the whole batch.
func evalCompiledBatchFn(args []js.Value) (any, error) {
	var doc struct {
		Handle   int              `json:"handle"`
		Decision string           `json:"decision"`
		Inputs   []map[string]any `json:"inputs"`
		Explain  bool             `json:"explain"`
		Full     bool             `json:"full"`
	}
	dec := json.NewDecoder(strings.NewReader(argStr(args, 0)))
	dec.UseNumber() // decimal exactness of input numbers (mirror evalCompiledFn)
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Decision == "" {
		return nil, errors.New("`decision` field required")
	}
	cm, err := getModel(doc.Handle)
	if err != nil {
		return nil, err
	}
	results := make([]any, len(doc.Inputs))
	for i, in := range doc.Inputs {
		r, err := evalResult(cm, doc.Decision, in, doc.Explain, doc.Full)
		if err != nil {
			results[i] = map[string]any{"error": err.Error()}
			continue
		}
		results[i] = r
	}
	return map[string]any{"decision": doc.Decision, "results": results}, nil
}

// infoCompiledFn: {handle} -> {name,inputs,decisions}. The handle twin of modelFn.
func infoCompiledFn(args []js.Value) (any, error) {
	h, err := handleArg(args)
	if err != nil {
		return nil, err
	}
	cm, err := getModel(h)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": cm.Name, "inputs": modelinfo.Inputs(cm), "decisions": modelinfo.Decisions(cm)}, nil
}

// requiredCompiledFn: {handle,decision} -> {decision,inputs}. The handle twin of requiredFn.
func requiredCompiledFn(args []js.Value) (any, error) {
	var doc struct {
		Handle   int    `json:"handle"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(argStr(args, 0)), &doc); err != nil {
		return nil, err
	}
	cm, err := getModel(doc.Handle)
	if err != nil {
		return nil, err
	}
	return requiredFor(cm, doc.Decision)
}

// disposeFn: {handle} -> {ok}. Frees a compiled model. Idempotent (disposing an unknown handle is a no-op).
func disposeFn(args []js.Value) (any, error) {
	h, err := handleArg(args)
	if err != nil {
		return nil, err
	}
	delete(models, h)
	return map[string]any{"ok": true}, nil
}

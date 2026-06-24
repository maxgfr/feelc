//go:build js && wasm

// Command feelc-wasm exposes the deterministic feelc engine to the browser as a global `feelc`
// object whose functions mirror the HTTP service (verify/run/graph/trace/model/required/check).
// Everything runs client-side: the playground compiles, verifies and evaluates `.rules` with the
// REAL engine — byte-for-byte identical to the CLI, no backend, no LLM. AI authoring stays in
// `feelc serve --ui`. Each function takes/returns JSON strings, recovers from panics, and returns
// {"error": ...} (structured diag.Error on a compile failure).
package main

import (
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
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/modelinfo"
	"github.com/maxgfr/feelc/internal/trace"
)

func main() {
	feelc := js.Global().Get("Object").New()
	feelc.Set("verify", wrap(verifyFn))
	feelc.Set("run", wrap(runFn))
	feelc.Set("graph", wrap(graphFn))
	feelc.Set("trace", wrap(traceFn))
	feelc.Set("model", wrap(modelFn))
	feelc.Set("required", wrap(requiredFn))
	feelc.Set("check", wrap(checkFn))
	feelc.Set("ready", js.ValueOf(true))
	js.Global().Set("feelc", feelc)
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
	out, err := engine.Eval(cm, doc.Decision, doc.Input)
	if err != nil {
		return nil, err
	}
	resp := map[string]any{"decision": doc.Decision, "output": modelinfo.JSONify(out)}
	switch {
	case doc.Full:
		if ft, e := explain.ExplainFull(cm, doc.Decision, doc.Input); e == nil {
			resp["trace"] = explain.NormalizeFullJSON(ft) // decimals as fixed-notation numbers, like `output`
		} else {
			resp["traceError"] = e.Error() // never silently drop the trace; `output` is already returned
		}
	case doc.Explain:
		if tr, e := explain.Explain(cm, doc.Decision, doc.Input); e == nil {
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
	req, err := cm.RequiredInputs(doc.Decision)
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
	return map[string]any{"decision": doc.Decision, "inputs": out}, nil
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

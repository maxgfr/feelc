// Package mcp exposes feelc as a Model Context Protocol server over stdio, so any MCP-capable agent
// (Claude, etc.) can author rules and have the DETERMINISTIC engine verify/run/explain them — the LLM
// writes, feelc decides (ADR 0008). Transport: newline-delimited JSON-RPC 2.0 on stdin/stdout. Each
// tool wraps the SAME core packages the CLI and WASM build use (loader/engine/verify/explain/graph/
// check/modelinfo), so an MCP result is byte-identical to `feelc run`/`verify`/… for the same input.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/graph"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/modelinfo"
	"github.com/maxgfr/feelc/internal/verify"
)

const protocolVersion = "2024-11-05"

// rpcRequest is a JSON-RPC 2.0 request (or notification when ID is absent).
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// tool is one exposed MCP tool: a name, an LLM-facing description, a JSON-Schema for its arguments,
// and the handler that runs it against the core engine.
type tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Handle      func(args map[string]any) (any, error)
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// tools is the fixed tool set. `rules` is always the feelc DSL source; the engine is the oracle.
func tools() []tool {
	rules := strProp("The feelc .rules DSL source (a model + inputs + decisions).")
	return []tool{
		{
			Name:        "feelc_verify",
			Description: "Compile and statically VERIFY a feelc model: reports completeness gaps, rule conflicts, dead rules and subsumption with counterexamples. Returns {hash, findings, blockers}. blockers==0 means the model is buildable. Call this before trusting any model.",
			Schema:      obj(map[string]any{"rules": rules}, "rules"),
			Handle: func(a map[string]any) (any, error) {
				cm, hash, rep, err := compile(a)
				if err != nil {
					return nil, err
				}
				_ = cm
				return map[string]any{"hash": hash, "findings": rep.Findings, "blockers": rep.Blockers()}, nil
			},
		},
		{
			Name:        "feelc_run",
			Description: "Evaluate a decision deterministically against concrete inputs. Returns {decision, output}. Numbers are exact decimals. NEVER decide a rule outcome yourself — run it.",
			Schema: obj(map[string]any{
				"rules":    rules,
				"decision": strProp("Name of the decision to evaluate."),
				"input":    map[string]any{"type": "object", "description": "Input name → value map."},
			}, "rules", "decision"),
			Handle: func(a map[string]any) (any, error) {
				src, dec, err := srcDecision(a)
				if err != nil {
					return nil, err
				}
				out, err := engine.Run(src, dec, inputMap(a["input"]))
				if err != nil {
					return nil, err
				}
				return map[string]any{"decision": dec, "output": modelinfo.JSONify(out)}, nil
			},
		},
		{
			Name:        "feelc_explain",
			Description: "Evaluate a decision AND return a deterministic justification trace: which rule fired, the cell values, and the contributing decisions. Use to explain WHY an output was produced.",
			Schema: obj(map[string]any{
				"rules":    rules,
				"decision": strProp("Name of the decision to explain."),
				"input":    map[string]any{"type": "object", "description": "Input name → value map."},
			}, "rules", "decision"),
			Handle: func(a map[string]any) (any, error) {
				cm, _, _, err := compile(a)
				if err != nil {
					return nil, err
				}
				dec, _ := a["decision"].(string)
				tr, err := explain.Explain(cm, dec, inputMap(a["input"]))
				if err != nil {
					return nil, err
				}
				return tr, nil
			},
		},
		{
			Name:        "feelc_required",
			Description: "List the inputs a decision transitively needs (question-flow): only ask the user for these. Returns {decision, inputs:[{name,type,...}]}.",
			Schema: obj(map[string]any{
				"rules":    rules,
				"decision": strProp("Name of the decision."),
			}, "rules", "decision"),
			Handle: func(a map[string]any) (any, error) {
				cm, _, _, err := compile(a)
				if err != nil {
					return nil, err
				}
				dec, _ := a["decision"].(string)
				req, err := cm.RequiredInputs(dec)
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
				return map[string]any{"decision": dec, "inputs": out}, nil
			},
		},
		{
			Name:        "feelc_check",
			Description: "Deterministically CHECK natural-language claims about a model against the engine. Each claim is {decision, input, expect}; returns a verdict per claim (supported / contradicted / error). Use to gate authoring: a claim that contradicts the model is a blocker.",
			Schema: obj(map[string]any{
				"rules":  rules,
				"claims": map[string]any{"type": "array", "description": "Array of {decision, input, expect[, desc]} claims."},
			}, "rules", "claims"),
			Handle: func(a map[string]any) (any, error) {
				cm, _, _, err := compile(a)
				if err != nil {
					return nil, err
				}
				claims, err := decodeClaims(a["claims"])
				if err != nil {
					return nil, err
				}
				rep := check.Check(cm, claims)
				return map[string]any{"report": rep, "blockers": rep.Blockers()}, nil
			},
		},
		{
			Name:        "feelc_graph",
			Description: "Return the decision-requirements graph (Mermaid + DOT) plus verification findings, for visualizing a model's structure and dependencies.",
			Schema:      obj(map[string]any{"rules": rules}, "rules"),
			Handle: func(a map[string]any) (any, error) {
				cm, _, rep, err := compile(a)
				if err != nil {
					return nil, err
				}
				g := graph.Build(cm, rep)
				return map[string]any{"mermaid": g.Mermaid(), "dot": g.DOT(), "findings": rep.Findings, "blockers": rep.Blockers()}, nil
			},
		},
		{
			Name:        "feelc_model",
			Description: "Return the model surface: name, typed inputs (with domains), and decisions. Use to discover what a model needs and produces.",
			Schema:      obj(map[string]any{"rules": rules}, "rules"),
			Handle: func(a map[string]any) (any, error) {
				cm, _, _, err := compile(a)
				if err != nil {
					return nil, err
				}
				return map[string]any{"name": cm.Name, "inputs": modelinfo.Inputs(cm), "decisions": modelinfo.Decisions(cm)}, nil
			},
		},
	}
}

func compile(a map[string]any) (*ir.CompiledModel, string, *verify.Report, error) {
	src, ok := a["rules"].(string)
	if !ok || src == "" {
		return nil, "", nil, fmt.Errorf("`rules` (string) is required")
	}
	cm, hash, rep, err := loader.Compile([]byte(src))
	if err != nil {
		return nil, "", nil, err
	}
	return cm, hash, rep, nil
}

func srcDecision(a map[string]any) (string, string, error) {
	src, ok := a["rules"].(string)
	if !ok || src == "" {
		return "", "", fmt.Errorf("`rules` (string) is required")
	}
	dec, ok := a["decision"].(string)
	if !ok || dec == "" {
		return "", "", fmt.Errorf("`decision` (string) is required")
	}
	return src, dec, nil
}

func inputMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func decodeClaims(v any) ([]check.Claim, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var claims []check.Claim
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&claims); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}
	return claims, nil
}

// Serve runs the MCP server loop over in/out until EOF. Each line is one JSON-RPC message.
func Serve(in io.Reader, out io.Writer, version string) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // allow large rule sources on one line
	w := bufio.NewWriter(out)
	defer w.Flush()
	tl := tools()
	byName := map[string]tool{}
	for _, t := range tl {
		byName[t.Name] = t
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber() // keep input numbers exact across the boundary
		if err := dec.Decode(&req); err != nil {
			writeResp(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error: " + err.Error()}})
			continue
		}
		resp, isNotification := dispatch(req, tl, byName, version)
		if isNotification {
			continue // notifications get no response
		}
		writeResp(w, resp)
	}
	return sc.Err()
}

func dispatch(req rpcRequest, tl []tool, byName map[string]tool, version string) (rpcResponse, bool) {
	base := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		base.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "feelc", "version": version},
		}
		return base, false
	case "notifications/initialized", "notifications/cancelled":
		return base, true // notification: no response
	case "ping":
		base.Result = map[string]any{}
		return base, false
	case "tools/list":
		list := make([]map[string]any, 0, len(tl))
		for _, t := range tl {
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
		}
		base.Result = map[string]any{"tools": list}
		return base, false
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		dec := json.NewDecoder(strings.NewReader(string(req.Params)))
		dec.UseNumber()
		if err := dec.Decode(&p); err != nil {
			base.Error = &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
			return base, false
		}
		t, ok := byName[p.Name]
		if !ok {
			base.Error = &rpcError{Code: -32601, Message: "unknown tool: " + p.Name}
			return base, false
		}
		base.Result = callTool(t, p.Arguments)
		return base, false
	default:
		base.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
		return base, false
	}
}

// callTool runs a tool and packages the result as MCP content. A tool error is returned as an MCP
// tool error (isError:true) carrying the structured message — NOT a protocol-level error — so the
// agent sees and can repair compile/verify failures (the red→green authoring loop).
func callTool(t tool, args map[string]any) map[string]any {
	res, err := t.Handle(args)
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}
	}
	b, mErr := json.MarshalIndent(res, "", "  ")
	if mErr != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": mErr.Error()}}, "isError": true}
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
}

func writeResp(w *bufio.Writer, resp rpcResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	w.Write(b)
	w.WriteByte('\n')
	w.Flush()
}

package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func runServer(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer
	if err := Serve(in, &out, "test"); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resps []map[string]any
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad response line %q: %v", sc.Text(), err)
		}
		resps = append(resps, m)
	}
	return resps
}

func callReq(id int, name string, args map[string]any) string {
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args}}
	b, _ := json.Marshal(req)
	return string(b)
}

func contentJSON(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", resp)
	}
	content := res["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("content not JSON (%q): %v", text, err)
	}
	return m
}

// initialize + a notification (no response) + tools/list.
func TestInitializeAndToolsList(t *testing.T) {
	resps := runServer(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(resps) != 2 { // the notification yields no response
		t.Fatalf("want 2 responses (notification is silent), got %d", len(resps))
	}
	si := resps[0]["result"].(map[string]any)["serverInfo"].(map[string]any)
	if si["name"] != "feelc" {
		t.Errorf("serverInfo.name = %v, want feelc", si["name"])
	}
	tl := resps[1]["result"].(map[string]any)["tools"].([]any)
	if len(tl) != 7 {
		t.Errorf("want 7 tools, got %d", len(tl))
	}
}

// tools/call: verify (blockers), run (deterministic output), and a bad model -> isError.
func TestToolsCallRunVerify(t *testing.T) {
	model := "model \"m\" {}\ninput n : number\ndecision tier : number = if n >= 700 then 1 else 0"
	resps := runServer(t,
		callReq(1, "feelc_verify", map[string]any{"rules": model}),
		callReq(2, "feelc_run", map[string]any{"rules": model, "decision": "tier", "input": map[string]any{"n": 750}}),
		callReq(3, "feelc_run", map[string]any{"rules": "model bad {", "decision": "x", "input": map[string]any{}}),
	)
	if v := contentJSON(t, resps[0]); v["blockers"] == nil {
		t.Errorf("verify result missing blockers: %v", v)
	}
	if r := contentJSON(t, resps[1]); fmt.Sprint(r["output"]) != "1" {
		t.Errorf("run output = %v, want 1", r["output"])
	}
	if resps[2]["result"].(map[string]any)["isError"] != true {
		t.Errorf("bad model must be a tool error (isError:true): %v", resps[2])
	}
}

// tools/call against the new features (power, OUTPUT ORDER, bounded quantifier, string predicate),
// proving the MCP tools share the engine's expanded surface.
func TestToolsCallNewFeatures(t *testing.T) {
	model := strings.Join([]string{
		`model "m" {}`,
		`input s : number`,
		`input a : number`,
		`input code : string`,
		`decision g : number = power(s, 2)`,
		`decision q : boolean = every of {a, s} satisfies ? > 0`,
		`decision e : boolean = starts_with(code, "EU")`,
	}, "\n")
	resps := runServer(t,
		callReq(1, "feelc_run", map[string]any{"rules": model, "decision": "g", "input": map[string]any{"s": 12}}),
		callReq(2, "feelc_run", map[string]any{"rules": model, "decision": "q", "input": map[string]any{"a": 5, "s": 7}}),
		callReq(3, "feelc_run", map[string]any{"rules": model, "decision": "e", "input": map[string]any{"code": "EU-9"}}),
	)
	if r := contentJSON(t, resps[0]); fmt.Sprint(r["output"]) != "144" {
		t.Errorf("power(12,2) via MCP = %v, want 144", r["output"])
	}
	if r := contentJSON(t, resps[1]); r["output"] != true {
		t.Errorf("bounded quantifier via MCP = %v, want true", r["output"])
	}
	if r := contentJSON(t, resps[2]); r["output"] != true {
		t.Errorf("starts_with via MCP = %v, want true", r["output"])
	}
}

// unknown method -> JSON-RPC error; unknown tool -> tool error.
func TestErrors(t *testing.T) {
	resps := runServer(t,
		`{"jsonrpc":"2.0","id":1,"method":"no/such/method"}`,
		callReq(2, "feelc_nope", map[string]any{}),
	)
	if resps[0]["error"] == nil {
		t.Errorf("unknown method should return a JSON-RPC error")
	}
	if resps[1]["error"] == nil {
		t.Errorf("unknown tool should return a JSON-RPC error")
	}
}

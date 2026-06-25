package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// parse is a small helper: unmarshal merged JSON bytes into a generic map for assertions.
func parse(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("merged output is not valid JSON: %v\n%s", err, b)
	}
	return m
}

func servers(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	s, ok := m["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("no mcpServers object in %v", m)
	}
	return s
}

func TestMergeMCPConfig_EmptyFile(t *testing.T) {
	out, changed, err := mergeMCPConfig(nil, "feelc", "feelc", false)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on a fresh config")
	}
	srv, ok := servers(t, parse(t, out))["feelc"].(map[string]any)
	if !ok {
		t.Fatalf("feelc server not present in merged output")
	}
	if srv["command"] != "feelc" {
		t.Errorf("command = %v, want feelc", srv["command"])
	}
	args, _ := srv["args"].([]any)
	if len(args) != 1 || args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp]", srv["args"])
	}
}

func TestMergeMCPConfig_PreservesSiblings(t *testing.T) {
	existing := []byte(`{"mcpServers":{"other":{"command":"x","args":["serve"]}},"misc":true}`)
	out, changed, err := mergeMCPConfig(existing, "feelc", "/usr/local/bin/feelc", false)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when adding a new server")
	}
	m := parse(t, out)
	if m["misc"] != true {
		t.Errorf("sibling top-level key 'misc' lost: %v", m["misc"])
	}
	srv := servers(t, m)
	if _, ok := srv["other"]; !ok {
		t.Errorf("sibling server 'other' was clobbered: %v", srv)
	}
	feelc, ok := srv["feelc"].(map[string]any)
	if !ok {
		t.Fatalf("feelc server not added: %v", srv)
	}
	if feelc["command"] != "/usr/local/bin/feelc" {
		t.Errorf("command = %v, want /usr/local/bin/feelc", feelc["command"])
	}
}

func TestMergeMCPConfig_Idempotent(t *testing.T) {
	first, changed, err := mergeMCPConfig(nil, "feelc", "feelc", false)
	if err != nil || !changed {
		t.Fatalf("first merge: changed=%v err=%v", changed, err)
	}
	second, changed2, err := mergeMCPConfig(first, "feelc", "feelc", false)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if changed2 {
		t.Errorf("re-merging an already-present entry should be a no-op (changed=false)")
	}
	if string(first) != string(second) {
		t.Errorf("idempotent merge changed the bytes:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestMergeMCPConfig_ForceOverwrites(t *testing.T) {
	existing := []byte(`{"mcpServers":{"feelc":{"command":"old","args":["mcp"]}}}`)
	out, changed, err := mergeMCPConfig(existing, "feelc", "new", true)
	if err != nil || !changed {
		t.Fatalf("force merge: changed=%v err=%v", changed, err)
	}
	srv := servers(t, parse(t, out))
	feelc := srv["feelc"].(map[string]any)
	if feelc["command"] != "new" {
		t.Errorf("force did not overwrite command: %v", feelc["command"])
	}
}

func TestMergeMCPConfig_PresentNoForce(t *testing.T) {
	existing := []byte(`{"mcpServers":{"feelc":{"command":"old","args":["mcp"]}}}`)
	out, changed, err := mergeMCPConfig(existing, "feelc", "new", false)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if changed {
		t.Errorf("expected no-op when present and !force")
	}
	srv := servers(t, parse(t, out))
	feelc := srv["feelc"].(map[string]any)
	if feelc["command"] != "old" {
		t.Errorf("entry must be left untouched without --force: %v", feelc["command"])
	}
}

func TestMergeMCPConfig_InvalidJSON(t *testing.T) {
	if _, _, err := mergeMCPConfig([]byte("{not json"), "feelc", "feelc", false); err == nil {
		t.Fatalf("expected an error on invalid JSON input")
	}
}

func TestResolveConfigPath(t *testing.T) {
	if p, err := resolveConfigPath("project", ""); err != nil || p != ".mcp.json" {
		t.Errorf("project target = %q, %v; want .mcp.json", p, err)
	}
	if p, err := resolveConfigPath("anything", "/tmp/custom.json"); err != nil || p != "/tmp/custom.json" {
		t.Errorf("override = %q, %v; want /tmp/custom.json", p, err)
	}
	if _, err := resolveConfigPath("bogus", ""); err == nil {
		t.Errorf("expected error on unknown target")
	}
	// claude-desktop resolves to an absolute path ending in claude_desktop_config.json.
	p, err := resolveConfigPath("claude-desktop", "")
	if err != nil {
		t.Fatalf("claude-desktop: %v", err)
	}
	if !filepath.IsAbs(p) || !strings.HasSuffix(p, "claude_desktop_config.json") {
		t.Errorf("claude-desktop path = %q, want absolute …/claude_desktop_config.json", p)
	}
}

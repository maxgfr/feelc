package main

import (
	"encoding/json"
	"os"
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

func TestMCPAgentInstructions(t *testing.T) {
	got := mcpAgentInstructions("feelc", "/usr/local/bin/feelc")
	for _, want := range []string{
		"claude mcp add feelc -- /usr/local/bin/feelc mcp",
		"codex mcp add feelc -- /usr/local/bin/feelc mcp",
		"opencode mcp add feelc -- /usr/local/bin/feelc mcp",
		"--path <file>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q:\n%s", want, got)
		}
	}
	if got := mcpAgentInstructions("feelc", "/Applications/My Tools/feelc"); !strings.Contains(got, "-- '/Applications/My Tools/feelc' mcp") {
		t.Errorf("a path with a space must be shell-quoted:\n%s", got)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"feelc":                "feelc",
		"/usr/local/bin/feelc": "/usr/local/bin/feelc",
		`C:\feelc.exe`:         `'C:\feelc.exe'`,
		"/a b/feelc":           "'/a b/feelc'",
		"it's":                 `'it'\''s'`,
		"":                     "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCmdMCPInstall_PathMergesIdempotently(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "nested", "mcp.json")
	if err := cmdMCPInstall([]string{"--path", cfg, "--command", "feelc"}); err != nil {
		t.Fatalf("install --path: %v", err)
	}
	first, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if srv := servers(t, parse(t, first))["feelc"].(map[string]any); srv["command"] != "feelc" {
		t.Errorf("command = %v, want feelc", srv["command"])
	}
	if err := cmdMCPInstall([]string{"--path", cfg, "--command", "other"}); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if second, _ := os.ReadFile(cfg); string(second) != string(first) {
		t.Errorf("re-running without --force rewrote the file:\n%s", second)
	}
}

func TestCmdMCPInstall_NoPathWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	stdout, stderr := os.Stdout, os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	os.Stdout, os.Stderr = devnull, devnull
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	if err := cmdMCPInstall(nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("feelc mcp install without --path must write nothing, found %v", entries)
	}
}

func TestCmdMCPInstall_RejectsRemovedAndConflictingFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--target", "project"},
		{"--target=claude-desktop"},
		{"--print", "--path", "x.json"},
		{"stray"},
	} {
		if err := cmdMCPInstall(args); err == nil {
			t.Errorf("cmdMCPInstall(%q) succeeded, want an error", args)
		}
	}
}

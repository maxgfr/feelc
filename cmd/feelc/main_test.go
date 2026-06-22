package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

// feelc tck: succeeds on the fixtures (3 passed, 1 skip), and --min too high fails.
func TestCmdTck(t *testing.T) {
	suite := filepath.Join("..", "..", "testdata", "dmn-tck")
	out := captureStdout(t, func() {
		if err := cmdTck([]string{"--suite", suite}); err != nil {
			t.Fatalf("tck: %v", err)
		}
	})
	if !strings.Contains(out, "3 passed") || !strings.Contains(out, "100.0%") {
		t.Errorf("unexpected tck output: %q", out)
	}
	// --min 100 passes (conformance 100%); a threshold above is impossible -> we test the failure via
	// a threshold > 100 (never reachable) to verify the gate.
	_ = captureStdout(t, func() {
		if err := cmdTck([]string{"--suite", suite, "--min", "100.5"}); err == nil {
			t.Errorf("--min 100.5 should fail (conformance 100%%)")
		}
	})
}

// feelc fmt: stdout by default, -w idempotent, --check exit≠0 if not formatted.
func TestCmdFmt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.rules")
	src := "model \"m\" {}\ninput a : number\ndecision d : number {\n  needs: a\n  hit: first\n  >= 1 => 10\n  default => 0\n}\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// stdout: produces a non-empty output that reparses.
	out := captureStdout(t, func() {
		if err := cmdFmt([]string{"--rules", p}); err != nil {
			t.Fatalf("fmt stdout: %v", err)
		}
	})
	if !strings.Contains(out, "decision d : number") {
		t.Fatalf("unexpected fmt output: %q", out)
	}
	// -w twice: the file is stable on the 2nd pass (idempotence).
	if err := cmdFmt([]string{"--rules", p, "-w"}); err != nil {
		t.Fatal(err)
	}
	after1, _ := os.ReadFile(p)
	if err := cmdFmt([]string{"--rules", p, "-w"}); err != nil {
		t.Fatal(err)
	}
	after2, _ := os.ReadFile(p)
	if string(after1) != string(after2) {
		t.Errorf("fmt -w not idempotent:\n%s\n---\n%s", after1, after2)
	}
	// --check: on an already formatted file -> exit 0; otherwise error.
	if err := cmdFmt([]string{"--rules", p, "--check"}); err != nil {
		t.Errorf("--check on formatted file should succeed: %v", err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil { // restore the non-canonical version
		t.Fatal(err)
	}
	if err := cmdFmt([]string{"--rules", p, "--check"}); err == nil {
		t.Errorf("--check on non-formatted file should fail")
	}
}

// feelc compile produces a canonical .ir.bin, and run/verify can reload it
// (direct loading of the IR, without recompilation).
func TestCompileThenRunFromBinary(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "m.rules")
	binPath := filepath.Join(dir, "m.ir.bin")
	src := `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: first
  < 0 => "neg"
  -   => "pos"
}`
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdCompile([]string{"--rules", srcPath, "-o", binPath}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	data, err := os.ReadFile(binPath)
	if err != nil || !ir.IsEncoded(data) {
		t.Fatalf("the .ir.bin does not have the feelc magic (err=%v)", err)
	}
	out := captureStdout(t, func() {
		if err := cmdRun([]string{"--rules", binPath, "--decision", "d", "--input", `{"n": -5}`}); err != nil {
			t.Fatalf("run from .ir.bin: %v", err)
		}
	})
	if strings.TrimSpace(out) != "neg" {
		t.Fatalf("run from .ir.bin = %q, expected \"neg\"", strings.TrimSpace(out))
	}
}

// captureStdout redirects os.Stdout during the execution of fn and returns what was written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// With --json, a compilation error is rendered as a structured JSON object
// {file,line,col,code,message,suggestion} on stdout (consumable by the skill).
func TestCmdVerifyJSONErrorStructured(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.rules")
	if err := os.WriteFile(p, []byte("model \"m\" {}\nbogus instruction\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got error
	out := captureStdout(t, func() {
		got = cmdVerify([]string{"--rules", p, "--json"})
	})
	if got == nil {
		t.Fatal("expected error")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("invalid JSON output: %q (%v)", out, err)
	}
	if obj["message"] == nil {
		t.Errorf("expected message field, got %v", obj)
	}
	if obj["file"] != p {
		t.Errorf("file field = %v, expected %q", obj["file"], p)
	}
	if obj["line"] == nil {
		t.Errorf("expected line field, got %v", obj)
	}
}

// Proves that reading the inputs preserves EXACTNESS: 2^53+1 is not representable
// exactly in float64. If decodeInputs went through float64, the equality would fail ("miss").
// With UseNumber + exact decimal, it succeeds ("exact").
func TestDecodeInputsExactBeyondFloat64(t *testing.T) {
	in, err := decodeInputs(`{"score": 9007199254740993}`) // 2^53 + 1
	if err != nil {
		t.Fatal(err)
	}
	src := `model "m" {}
input score : number
decision d : string {
  needs: score
  hit: first
  9007199254740993 => "exact"
  -                => "miss"
}`
	got, err := engine.Run(src, "d", in)
	if err != nil {
		t.Fatal(err)
	}
	if got != "exact" {
		t.Errorf("got %v, expected \"exact\" (precision loss = input passed through float64)", got)
	}
}

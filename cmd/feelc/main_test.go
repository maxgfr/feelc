package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// captureStdout redirige os.Stdout pendant l'exécution de fn et renvoie ce qui y a été écrit.
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

// Avec --json, une erreur de compilation est rendue en objet JSON structuré
// {file,line,col,code,message,suggestion} sur stdout (consommable par la skill).
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
		t.Fatal("erreur attendue")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("sortie JSON invalide: %q (%v)", out, err)
	}
	if obj["message"] == nil {
		t.Errorf("champ message attendu, obtenu %v", obj)
	}
	if obj["file"] != p {
		t.Errorf("champ file = %v, attendu %q", obj["file"], p)
	}
	if obj["line"] == nil {
		t.Errorf("champ line attendu, obtenu %v", obj)
	}
}

// Prouve que la lecture des entrées préserve l'EXACTITUDE : 2^53+1 n'est pas représentable
// exactement en float64. Si decodeInputs passait par float64, l'égalité échouerait ("miss").
// Avec UseNumber + décimal exact, elle réussit ("exact").
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
		t.Errorf("got %v, attendu \"exact\" (perte de précision = entrée passée par float64)", got)
	}
}

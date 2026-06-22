// Command feelc : le binaire du moteur de règles feelc.
// Tranche 1 : sous-commande `run` (évaluer une décision sur des entrées). Les autres
// sous-commandes (compile/verify/check/explain/fmt/serve...) arrivent dans les tranches suivantes.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/verify"
)

// Version est injectée au build (ldflags) ; valeur par défaut pour le dev.
var Version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "run":
		err = cmdRun(args)
	case "verify":
		err = cmdVerify(args)
	case "version", "--version", "-v":
		fmt.Println("feelc", Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "feelc: sous-commande inconnue %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `feelc — moteur de règles métier (DMN/FEEL) compilé

Usage:
  feelc run    --rules <fichier.rules> --decision <nom> --input '<json>' [--json]
  feelc verify --rules <fichier.rules> [--json]
  feelc version

`)
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "chemin du fichier .rules")
	asJSON := fs.Bool("json", false, "sortie au format JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules est requis")
	}
	src, err := os.ReadFile(*rulesPath)
	if err != nil {
		return err
	}
	m, err := dsl.Parse(string(src))
	if err != nil {
		return err
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		return err
	}
	rep := verify.Verify(cm)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		renderReport(rep)
	}
	if n := rep.Blockers(); n > 0 {
		return fmt.Errorf("%d bloqueur(s) de vérification", n)
	}
	return nil
}

func renderReport(rep *verify.Report) {
	if len(rep.Findings) == 0 {
		fmt.Println("✓ aucune anomalie détectée (table prouvée complète et cohérente)")
		return
	}
	for _, f := range rep.Findings {
		fmt.Printf("[%s] %s — %s\n", f.Severity, f.Decision, f.Message)
		if len(f.Witness) > 0 {
			fmt.Printf("        contre-exemple: %v\n", f.Witness)
		}
	}
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "chemin du fichier .rules")
	decision := fs.String("decision", "", "nom de la décision à évaluer")
	inputJSON := fs.String("input", "{}", "données d'entrée au format JSON")
	asJSON := fs.Bool("json", false, "sortie au format JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" || *decision == "" {
		return fmt.Errorf("--rules et --decision sont requis")
	}
	src, err := os.ReadFile(*rulesPath)
	if err != nil {
		return err
	}
	inputs, err := decodeInputs(*inputJSON)
	if err != nil {
		return fmt.Errorf("--input: %w", err)
	}
	out, err := engine.Run(string(src), *decision, inputs)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(map[string]any{"decision": *decision, "output": display(out)})
	}
	fmt.Println(display(out))
	return nil
}



// decodeInputs lit le JSON d'entrée en préservant l'exactitude des nombres (UseNumber).
func decodeInputs(s string) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// display rend une valeur de sortie sous forme lisible/sérialisable.
func display(v any) any {
	if d, ok := v.(*apd.Decimal); ok {
		return d.Text('f')
	}
	return v
}

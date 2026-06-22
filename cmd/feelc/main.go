// Command feelc : le binaire du moteur de règles feelc.
// Tranche 1 : sous-commande `run` (évaluer une décision sur des entrées). Les autres
// sous-commandes (compile/verify/check/explain/fmt/serve...) arrivent dans les tranches suivantes.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/audit"
	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dmnxml"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/fmtrules"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
	"github.com/maxgfr/feelc/internal/verify"
)

// errAlreadyReported signale à main() qu'une erreur a déjà été rendue (ex: JSON
// structuré sur stdout) : exit 1 sans réémettre de texte.
var errAlreadyReported = errors.New("")

// reportCompileErr rend une erreur de compilation. En mode --json, si l'erreur est un
// *diag.Error, l'émet en objet JSON {file,line,col,code,message,suggestion} sur stdout
// (consommable par la skill) et renvoie errAlreadyReported ; sinon renvoie l'erreur telle
// quelle (rendue en texte « file:line:col: message » par main).
func reportCompileErr(err error, asJSON bool) error {
	var de *diag.Error
	if asJSON && errors.As(err, &de) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(de)
		return errAlreadyReported
	}
	return err
}

// loadModel lit un modèle depuis un chemin : soit une source .rules (parse + compile, avec
// erreurs positionnées), soit un .ir.bin déjà compilé (décodage direct de l'IR canonique).
// Renvoie aussi le rapport de vérification (recalculé pour un binaire). En cas d'erreur de
// compilation et --json, l'erreur structurée a déjà été rendue (errAlreadyReported).
func loadModel(path string, asJSON bool) (*ir.CompiledModel, *verify.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if ir.IsEncoded(data) {
		cm, err := ir.Decode(data)
		if err != nil {
			return nil, nil, err
		}
		return cm, verify.Verify(cm), nil
	}
	cm, _, rep, err := loader.CompileFile(path, data)
	if err != nil {
		return nil, nil, reportCompileErr(err, asJSON)
	}
	return cm, rep, nil
}

// cmdCompile compile une source .rules en IR canonique sérialisé (.ir.bin) et affiche le hash.
func cmdCompile(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "chemin du fichier .rules")
	out := fs.String("o", "", "fichier .ir.bin de sortie (stdout si absent)")
	asJSON := fs.Bool("json", false, "sortie au format JSON pour les erreurs")
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
	cm, _, _, err := loader.CompileFile(*rulesPath, src)
	if err != nil {
		return reportCompileErr(err, *asJSON)
	}
	blob, err := ir.Encode(cm)
	if err != nil {
		return err
	}
	h, err := ir.Hash(cm)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "modèle %q compilé — %d octets — hash %s\n", cm.Name, len(blob), hex.EncodeToString(h[:]))
	if *out == "" {
		_, err = os.Stdout.Write(blob)
		return err
	}
	return os.WriteFile(*out, blob, 0o644)
}

// Version est injectée au build (ldflags) ; valeur par défaut pour le dev.
var Version = "0.0.0-dev"

func main() {
	applyMemoryLimit()
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "run":
		err = cmdRun(args)
	case "compile":
		err = cmdCompile(args)
	case "verify":
		err = cmdVerify(args)
	case "explain":
		err = cmdExplain(args)
	case "check":
		err = cmdCheck(args)
	case "fmt":
		err = cmdFmt(args)
	case "import":
		err = cmdImport(args)
	case "serve":
		err = cmdServe(args)
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
		if !errors.Is(err, errAlreadyReported) {
			fmt.Fprintln(os.Stderr, "erreur:", err)
		}
		os.Exit(1)
	}
}

// applyMemoryLimit borne la RAM du process (garde-fou : un .rules pathologique ne doit jamais
// faire exploser la mémoire). Soft limit GC (runtime/debug). Si l'utilisateur a déjà fixé
// GOMEMLIMIT, le runtime l'applique nativement et on ne le surcharge pas ; sinon on impose un
// plafond par défaut (généreux : invisible en usage normal), ajustable via FEELC_MEMLIMIT (octets).
func applyMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return // déjà appliqué nativement par le runtime
	}
	limit := int64(2 << 30) // 2 GiB par défaut
	if v := os.Getenv("FEELC_MEMLIMIT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	debug.SetMemoryLimit(limit)
}

func usage() {
	fmt.Fprint(os.Stderr, `feelc — moteur de règles métier (DMN/FEEL) compilé

Usage:
  feelc run     --rules <fichier.rules|.ir.bin> --decision <nom> --input '<json>' [--json]
  feelc compile --rules <fichier.rules> [-o <model.ir.bin>] [--json]
  feelc verify  --rules <fichier.rules|.ir.bin> [--json]
  feelc explain --rules <fichier.rules|.ir.bin> --decision <nom> --input '<json>' [--json]
  feelc check  --rules <fichier.rules> --claims <claims.json> [--json]
  feelc fmt    --rules <fichier.rules> [-w] [--check]
  feelc import --in <modele.dmn> [-o <sortie.rules>]
  feelc serve  --rules <fichier.rules> [--addr :8080] [--watch] [--strict]
  feelc version

Environnement:
  FEELC_MEMLIMIT  plafond mémoire (octets) du process (défaut 2 GiB ; GOMEMLIMIT a priorité)

`)
}

func cmdFmt(args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "chemin du fichier .rules")
	write := fs.Bool("w", false, "réécrire le fichier en place")
	check := fs.Bool("check", false, "échoue (exit≠0) si le fichier n'est pas déjà formaté ; n'écrit pas")
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
	formatted, err := fmtrules.Source(string(src))
	if err != nil {
		return err
	}
	// Signaler les pertes (jamais conformer en silence) — sur stderr, pas dans la sortie.
	if strings.Contains(string(src), "#") || strings.Contains(string(src), "rounding:") {
		fmt.Fprintln(os.Stderr, "note: `feelc fmt` ne préserve pas les commentaires ni le corps du bloc `model { ... }`")
	}
	switch {
	case *check:
		if string(src) != formatted {
			return fmt.Errorf("%s n'est pas formaté (lancez `feelc fmt -w %s`)", *rulesPath, *rulesPath)
		}
		return nil
	case *write:
		return os.WriteFile(*rulesPath, []byte(formatted), 0o644)
	default:
		fmt.Print(formatted)
		return nil
	}
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	in := fs.String("in", "", "chemin du fichier DMN XML à importer")
	out := fs.String("o", "", "fichier .rules de sortie (stdout si absent)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in est requis")
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	rules, warns, err := dmnxml.Import(data)
	if err != nil {
		return err
	}
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "avertissement:", w)
	}
	if *out == "" {
		fmt.Print(rules)
		return nil
	}
	return os.WriteFile(*out, []byte(rules), 0o644)
}

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "chemin du fichier .rules")
	claimsPath := fs.String("claims", "", "chemin du fichier de claims JSON (produit par l'IA)")
	asJSON := fs.Bool("json", false, "sortie au format JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" || *claimsPath == "" {
		return fmt.Errorf("--rules et --claims sont requis")
	}
	cm, _, err := loadModel(*rulesPath, *asJSON)
	if err != nil {
		return err
	}
	cf, err := os.ReadFile(*claimsPath)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(cf))
	dec.UseNumber() // exactitude des nombres attendus
	var doc struct {
		Claims []check.Claim `json:"claims"`
	}
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("claims invalides: %w", err)
	}
	rep := check.Check(cm, doc.Claims)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		for _, v := range rep.Verdicts {
			line := fmt.Sprintf("[%s] %s", v.Status, v.Claim.Decision)
			if v.Claim.Desc != "" {
				line += " — " + v.Claim.Desc
			}
			if v.Detail != "" {
				line += " (" + v.Detail + ")"
			}
			fmt.Println(line)
		}
	}
	if n := rep.Blockers(); n > 0 {
		return fmt.Errorf("%d claim(s) non supporté(s)", n)
	}
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "chemin du fichier .rules à servir")
	addr := fs.String("addr", ":8080", "adresse d'écoute HTTP")
	watch := fs.Bool("watch", false, "recharger à chaud sur modification du fichier")
	strict := fs.Bool("strict", false, "refuser le (re)chargement si la vérification a des bloqueurs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules est requis")
	}

	reg := registry.New()
	logReload := func(e *registry.Entry, rep *verify.Report, err error) {
		switch {
		case err != nil:
			fmt.Fprintln(os.Stderr, "reload refusé (modèle courant conservé):", err)
		case rep != nil && len(rep.Findings) > 0:
			fmt.Fprintf(os.Stderr, "modèle v%d chargé (%s) — %d remarque(s) de vérification\n", e.Version, e.Hash, len(rep.Findings))
		default:
			fmt.Fprintf(os.Stderr, "modèle v%d chargé (%s) — vérification propre\n", e.Version, e.Hash)
		}
	}

	// Chargement initial : doit réussir.
	e, rep, err := loader.Reload(*rulesPath, reg, *strict)
	if err != nil {
		return fmt.Errorf("chargement initial: %w", err)
	}
	logReload(e, rep, nil)

	if *watch {
		stop, err := loader.Watch(*rulesPath, reg, *strict, logReload)
		if err != nil {
			return err
		}
		defer stop()
	}

	reloadFn := func() error {
		e, rep, err := loader.Reload(*rulesPath, reg, *strict)
		logReload(e, rep, err)
		return err
	}
	srv := service.New(reg, audit.New(os.Stderr), reloadFn)
	fmt.Fprintf(os.Stderr, "feelc serve sur %s (modèle %q)\n", *addr, e.Model.Name)
	return http.ListenAndServe(*addr, srv.Handler())
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
	_, rep, err := loadModel(*rulesPath, *asJSON)
	if err != nil {
		return err
	}

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
	inputs, err := decodeInputs(*inputJSON)
	if err != nil {
		return fmt.Errorf("--input: %w", err)
	}
	cm, _, err := loadModel(*rulesPath, *asJSON)
	if err != nil {
		return err
	}
	out, err := engine.Eval(cm, *decision, inputs)
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



func cmdExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "chemin du fichier .rules ou .ir.bin")
	decision := fs.String("decision", "", "nom de la décision à expliquer")
	inputJSON := fs.String("input", "{}", "données d'entrée au format JSON")
	asJSON := fs.Bool("json", false, "sortie au format JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" || *decision == "" {
		return fmt.Errorf("--rules et --decision sont requis")
	}
	inputs, err := decodeInputs(*inputJSON)
	if err != nil {
		return fmt.Errorf("--input: %w", err)
	}
	cm, _, err := loadModel(*rulesPath, *asJSON)
	if err != nil {
		return err
	}
	tr, err := explain.Explain(cm, *decision, inputs)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tr)
	}
	renderTrace(tr)
	return nil
}

// renderTrace rend une trace de justification de façon lisible.
func renderTrace(tr *explain.Trace) {
	fmt.Printf("décision %q = %v\n", tr.Decision, display(tr.Output))
	switch {
	case tr.Kind == "literal-expr":
		fmt.Printf("  expression : %s  (évaluée, justification non géométrique)\n", tr.ExprSrc)
	case len(tr.Contributors) > 0:
		fmt.Printf("  hit policy %s — règles contributrices :\n", tr.HitPolicy)
		for _, c := range tr.Contributors {
			fmt.Printf("    • règle #%d (ligne %d)\n", c.Index, c.Line)
		}
	case tr.Fallback:
		fmt.Println("  aucune règle ne matche → ligne `default` (ou null)")
	case tr.Matched:
		fmt.Printf("  règle #%d (ligne %d) — hit policy %s\n", tr.RuleIndex, tr.RuleLine, tr.HitPolicy)
		for _, c := range tr.Cells {
			fmt.Printf("    • %s %s  (valeur: %s, ligne %d)\n", c.Input, c.Src, c.Value, c.Line)
		}
		if tr.NotGeometric {
			fmt.Println("    (une cellule est une expression : justification évaluée, non géométrique)")
		}
	}
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

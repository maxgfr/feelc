// Command feelc: the feelc rules engine binary.
// Slice 1: `run` subcommand (evaluate a decision against inputs). The other
// subcommands (compile/verify/check/explain/fmt/serve...) arrive in later slices.
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
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/fmtrules"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
	"github.com/maxgfr/feelc/internal/tck"
	"github.com/maxgfr/feelc/internal/verify"
)

// errAlreadyReported signals to main() that an error has already been rendered (e.g. structured
// JSON on stdout): exit 1 without re-emitting any text.
var errAlreadyReported = errors.New("")

// reportCompileErr renders a compilation error. In --json mode, if the error is a
// *diag.Error, it emits it as a JSON object {file,line,col,code,message,suggestion} on stdout
// (consumable by the skill) and returns errAlreadyReported; otherwise it returns the error as
// is (rendered as text "file:line:col: message" by main).
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

// loadModel reads a model from a path: either a .rules source (parse + compile, with positioned
// errors), or an already-compiled .ir.bin (direct decoding of the canonical IR).
// It also returns the verification report (recomputed for a binary). On compilation error with
// --json, the structured error has already been rendered (errAlreadyReported).
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

// cmdCompile compiles a .rules source into serialized canonical IR (.ir.bin) and prints the hash.
func cmdCompile(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file")
	out := fs.String("o", "", "output .ir.bin file (stdout if absent)")
	asJSON := fs.Bool("json", false, "JSON output format for errors")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules is required")
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
	fmt.Fprintf(os.Stderr, "model %q compiled — %d bytes — hash %s\n", cm.Name, len(blob), hex.EncodeToString(h[:]))
	if *out == "" {
		_, err = os.Stdout.Write(blob)
		return err
	}
	return os.WriteFile(*out, blob, 0o644)
}

// Version is injected at build time (ldflags); default value for dev.
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
	case "export":
		err = cmdExport(args)
	case "tck":
		err = cmdTck(args)
	case "serve":
		err = cmdServe(args)
	case "version", "--version", "-v":
		fmt.Println("feelc", Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "feelc: unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		if !errors.Is(err, errAlreadyReported) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

// applyMemoryLimit bounds the process RAM (safeguard: a pathological .rules must never blow up
// memory). GC soft limit (runtime/debug). If the user has already set GOMEMLIMIT, the runtime
// applies it natively and we do not override it; otherwise we impose a default ceiling (generous:
// invisible in normal usage), adjustable via FEELC_MEMLIMIT (bytes).
func applyMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return // already applied natively by the runtime
	}
	limit := int64(2 << 30) // 2 GiB by default
	if v := os.Getenv("FEELC_MEMLIMIT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	debug.SetMemoryLimit(limit)
}

func usage() {
	fmt.Fprint(os.Stderr, `feelc — compiled business rules engine (DMN/FEEL)

Usage:
  feelc run     --rules <file.rules|.ir.bin> --decision <name> --input '<json>' [--json]
  feelc compile --rules <file.rules> [-o <model.ir.bin>] [--json]
  feelc verify  --rules <file.rules|.ir.bin> [--json]
  feelc explain --rules <file.rules|.ir.bin> --decision <name> --input '<json>' [--json]
  feelc check  --rules <file.rules> --claims <claims.json> [--json]
  feelc fmt    --rules <file.rules> [-w] [--check]
  feelc import --in <model.dmn> [-o <output.rules>]
  feelc export --rules <file.rules> [-o <output.dmn>]
  feelc tck    --suite <dir-tck> [--json] [--min <pct>]
  feelc serve  --rules <file.rules> [--addr :8080] [--watch] [--strict]
  feelc version

Environment:
  FEELC_MEMLIMIT  process memory ceiling (bytes) (default 2 GiB; GOMEMLIMIT takes priority)

`)
}

func cmdFmt(args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file")
	write := fs.Bool("w", false, "rewrite the file in place")
	check := fs.Bool("check", false, "fail (exit≠0) if the file is not already formatted; does not write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules is required")
	}
	src, err := os.ReadFile(*rulesPath)
	if err != nil {
		return err
	}
	formatted, err := fmtrules.Source(string(src))
	if err != nil {
		return err
	}
	// Report losses (never silently conform) — on stderr, not in the output.
	if strings.Contains(string(src), "#") || strings.Contains(string(src), "rounding:") {
		fmt.Fprintln(os.Stderr, "note: `feelc fmt` does not preserve comments or the body of the `model { ... }` block")
	}
	switch {
	case *check:
		if string(src) != formatted {
			return fmt.Errorf("%s is not formatted (run `feelc fmt -w %s`)", *rulesPath, *rulesPath)
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
	in := fs.String("in", "", "path to the DMN XML file to import")
	out := fs.String("o", "", "output .rules file (stdout if absent)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in is required")
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
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if *out == "" {
		fmt.Print(rules)
		return nil
	}
	return os.WriteFile(*out, []byte(rules), 0o644)
}

func cmdTck(args []string) error {
	fs := flag.NewFlagSet("tck", flag.ContinueOnError)
	suite := fs.String("suite", "", "directory of the DMN TCK suite")
	asJSON := fs.Bool("json", false, "JSON output format")
	min := fs.Float64("min", 0, "minimum conformance threshold (%); exit≠0 if below")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *suite == "" {
		return fmt.Errorf("--suite is required")
	}
	rep, err := tck.Run(*suite)
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
		renderTckReport(rep)
	}
	if rep.Failed > 0 {
		return fmt.Errorf("%d TCK cases failed", rep.Failed)
	}
	if *min > 0 && rep.Conformance() < *min {
		return fmt.Errorf("conformance %.1f%% < threshold %.1f%%", rep.Conformance(), *min)
	}
	return nil
}

func renderTckReport(rep *tck.Report) {
	fmt.Printf("TCK: %d passed, %d failed, %d skipped — conformance %.1f%%\n",
		rep.Passed, rep.Failed, rep.Skipped, rep.Conformance())
	for _, c := range rep.Cases {
		switch c.Status {
		case tck.Fail:
			fmt.Printf("  ✗ %s/%s [%s] expected %s, got %s\n", c.Model, c.Case, c.Decision, c.Expected, c.Got)
		case tck.Skipped:
			fmt.Printf("  ⊘ %s/%s [%s] skipped: %s\n", c.Model, c.Case, c.Decision, c.Reason)
		}
	}
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file to export to DMN")
	out := fs.String("o", "", "output .dmn file (stdout if absent)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules is required")
	}
	src, err := os.ReadFile(*rulesPath)
	if err != nil {
		return err
	}
	m, err := dsl.ParseFile(*rulesPath, string(src))
	if err != nil {
		return err
	}
	xmlOut, warns, err := dmnxml.Export(m)
	if err != nil {
		return err
	}
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if *out == "" {
		_, err = os.Stdout.Write(xmlOut)
		return err
	}
	return os.WriteFile(*out, xmlOut, 0o644)
}

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file")
	claimsPath := fs.String("claims", "", "path to the JSON claims file (produced by the AI)")
	asJSON := fs.Bool("json", false, "JSON output format")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" || *claimsPath == "" {
		return fmt.Errorf("--rules and --claims are required")
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
	dec.UseNumber() // exactness of expected numbers
	var doc struct {
		Claims []check.Claim `json:"claims"`
	}
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("invalid claims: %w", err)
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
		return fmt.Errorf("%d claim(s) not supported", n)
	}
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file to serve")
	addr := fs.String("addr", ":8080", "HTTP listen address")
	watch := fs.Bool("watch", false, "hot reload on file modification")
	strict := fs.Bool("strict", false, "refuse (re)loading if verification has blockers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules is required")
	}

	reg := registry.New()
	logReload := func(e *registry.Entry, rep *verify.Report, err error) {
		switch {
		case err != nil:
			fmt.Fprintln(os.Stderr, "reload refused (current model kept):", err)
		case rep != nil && len(rep.Findings) > 0:
			fmt.Fprintf(os.Stderr, "model v%d loaded (%s) — %d verification finding(s)\n", e.Version, e.Hash, len(rep.Findings))
		default:
			fmt.Fprintf(os.Stderr, "model v%d loaded (%s) — clean verification\n", e.Version, e.Hash)
		}
	}

	// Initial load: must succeed.
	e, rep, err := loader.Reload(*rulesPath, reg, *strict)
	if err != nil {
		return fmt.Errorf("initial load: %w", err)
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
	fmt.Fprintf(os.Stderr, "feelc serve on %s (model %q)\n", *addr, e.Model.Name)
	return http.ListenAndServe(*addr, srv.Handler())
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file")
	asJSON := fs.Bool("json", false, "JSON output format")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules is required")
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
		return fmt.Errorf("%d verification blocker(s)", n)
	}
	return nil
}

func renderReport(rep *verify.Report) {
	if len(rep.Findings) == 0 {
		fmt.Println("✓ no anomaly detected (table proven complete and consistent)")
		return
	}
	for _, f := range rep.Findings {
		fmt.Printf("[%s] %s — %s\n", f.Severity, f.Decision, f.Message)
		if len(f.Witness) > 0 {
			fmt.Printf("        counter-example: %v\n", f.Witness)
		}
	}
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file")
	decision := fs.String("decision", "", "name of the decision to evaluate")
	inputJSON := fs.String("input", "{}", "input data in JSON format")
	asJSON := fs.Bool("json", false, "JSON output format")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" || *decision == "" {
		return fmt.Errorf("--rules and --decision are required")
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
	rulesPath := fs.String("rules", "", "path to the .rules or .ir.bin file")
	decision := fs.String("decision", "", "name of the decision to explain")
	inputJSON := fs.String("input", "{}", "input data in JSON format")
	asJSON := fs.Bool("json", false, "JSON output format")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" || *decision == "" {
		return fmt.Errorf("--rules and --decision are required")
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

// renderTrace renders a justification trace in a readable way.
func renderTrace(tr *explain.Trace) {
	fmt.Printf("decision %q = %v\n", tr.Decision, display(tr.Output))
	switch {
	case tr.Kind == "literal-expr":
		fmt.Printf("  expression: %s  (evaluated, non-geometric justification)\n", tr.ExprSrc)
	case len(tr.Contributors) > 0:
		fmt.Printf("  hit policy %s — contributing rules:\n", tr.HitPolicy)
		for _, c := range tr.Contributors {
			fmt.Printf("    • rule #%d (line %d)\n", c.Index, c.Line)
		}
	case tr.Fallback:
		fmt.Println("  no rule matches → `default` line (or null)")
	case tr.Matched:
		fmt.Printf("  rule #%d (line %d) — hit policy %s\n", tr.RuleIndex, tr.RuleLine, tr.HitPolicy)
		for _, c := range tr.Cells {
			fmt.Printf("    • %s %s  (value: %s, line %d)\n", c.Input, c.Src, c.Value, c.Line)
		}
		if tr.NotGeometric {
			fmt.Println("    (a cell is an expression: justification evaluated, non-geometric)")
		}
	}
}

// decodeInputs reads the input JSON preserving number exactness (UseNumber).
func decodeInputs(s string) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// display renders an output value in a readable/serializable form.
func display(v any) any {
	if d, ok := v.(*apd.Decimal); ok {
		return d.Text('f')
	}
	return v
}

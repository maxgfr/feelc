// Command feelc: the feelc rules engine binary. Subcommands: run, compile, verify, explain, check,
// fmt, import, export, tck, graph, inputs, docs, serve, healthcheck, version (run `feelc help` for the
// synopsis, or see docs/cli.md).
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/audit"
	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dmnxml"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/fmtrules"
	"github.com/maxgfr/feelc/internal/graph"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/project"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
	"github.com/maxgfr/feelc/internal/tck"
	"github.com/maxgfr/feelc/internal/trace"
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
	case "graph":
		err = cmdGraph(args)
	case "inputs":
		err = cmdInputs(args)
	case "docs":
		err = cmdDocs(args)
	case "serve":
		err = cmdServe(args)
	case "healthcheck":
		err = cmdHealthcheck(args)
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
  feelc graph  --rules <file.rules|.ir.bin> [--format mermaid|dot|json] [-o <file>]
  feelc inputs --rules <file.rules|.ir.bin> --decision <name> [--json]
  feelc docs   --rules <file.rules|.ir.bin> [-o <DOC.md>]
  feelc serve  --rules <file.rules> [--addr :8080] [--watch] [--strict] [--ui]
  feelc serve  --project <dir> [--addr :8080] [--watch] [--strict] [--ui] [--allow-edit]
  feelc healthcheck [--addr :8080]
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

// cmdInputs lists the inputs a decision transitively requires (question-flow / simulator).
func cmdInputs(args []string) error {
	fs := flag.NewFlagSet("inputs", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules|.ir.bin file")
	decision := fs.String("decision", "", "target decision")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" || *decision == "" {
		return fmt.Errorf("--rules and --decision are required")
	}
	cm, _, err := loadModel(*rulesPath, *asJSON)
	if err != nil {
		return err
	}
	req, err := cm.RequiredInputs(*decision)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"decision": *decision, "inputs": req})
	}
	fmt.Printf("decision %q requires %d input(s):\n", *decision, len(req))
	for _, n := range req {
		line := "  - " + n + " : " + inputTypeName(cm.Inputs[n])
		if q := cm.InputMeta[n].Question; q != "" {
			line += "   # " + q
		}
		fmt.Println(line)
	}
	return nil
}

func inputTypeName(t ir.Type) string {
	switch t {
	case ir.TypeNumber:
		return "number"
	case ir.TypeString:
		return "string"
	case ir.TypeBool:
		return "boolean"
	case ir.TypeDate:
		return "date"
	case ir.TypeDuration:
		return "duration"
	case ir.TypeContext:
		return "context"
	}
	return "value"
}

// cmdDocs generates a Markdown reference for a model: inputs (type/unit/domain/question), decisions
// (kind/hit/unit/deps/source) and an embedded Mermaid decision graph.
func cmdDocs(args []string) error {
	fs := flag.NewFlagSet("docs", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules|.ir.bin file")
	out := fs.String("o", "", "output file (stdout if absent)")
	traceFlag := fs.Bool("trace", false, "append a source<->rule traceability section")
	specPath := fs.String("spec", "", "raw specification text for heuristic source-span coverage (implies --trace)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules is required")
	}
	cm, rep, err := loadModel(*rulesPath, false)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Model: %s\n\n", cm.Name)

	b.WriteString("## Inputs\n\n| Name | Type | Unit | Domain | Question |\n|---|---|---|---|---|\n")
	inNames := make([]string, 0, len(cm.Inputs))
	for n := range cm.Inputs {
		inNames = append(inNames, n)
	}
	sortStrings(inNames)
	for _, n := range inNames {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
			n, inputTypeName(cm.Inputs[n]), cm.Units[n], mdDomain(cm.Domains[n]), cm.InputMeta[n].Question)
	}

	b.WriteString("\n## Decisions\n\n| Name | Kind | Hit policy | Unit | Depends on | Source |\n|---|---|---|---|---|---|\n")
	for i := range cm.Decisions {
		d := &cm.Decisions[i]
		kind, hp := "expression", ""
		if d.Kind == ir.KindTable && d.Table != nil {
			kind, hp = "table", hitPolicyDoc(d.Table.HitPolicy)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n",
			d.Name, kind, hp, cm.Units[d.Name], strings.Join(d.Deps, ", "), d.Meta.Source)
	}

	b.WriteString("\n## Decision graph\n\n```mermaid\n")
	b.WriteString(graph.Build(cm, rep).Mermaid())
	b.WriteString("```\n")

	if *traceFlag || *specPath != "" {
		if err := writeTraceability(&b, cm, *specPath); err != nil {
			return err
		}
	}

	if *out == "" {
		fmt.Print(b.String())
		return nil
	}
	return os.WriteFile(*out, []byte(b.String()), 0o644)
}

// writeTraceability appends the source<->rule traceability section to the docs: which decisions
// cite which @source, the untraced decisions, and (with a spec) heuristic source-span coverage.
func writeTraceability(b *strings.Builder, cm *ir.CompiledModel, specPath string) error {
	var rep *trace.Report
	if specPath != "" {
		spec, err := os.ReadFile(specPath)
		if err != nil {
			return err
		}
		rep = trace.BuildWithSource(cm, spec)
	} else {
		rep = trace.Build(cm)
	}
	b.WriteString("\n## Source traceability\n\n")
	fmt.Fprintf(b, "%d/%d decisions cite a source.\n\n", rep.Coverage.DecisionsSourced, rep.Coverage.Decisions)
	b.WriteString("| Decision | Source |\n|---|---|\n")
	for _, d := range rep.Decisions {
		src := d.Source
		if src == "" {
			src = "_(none)_"
		}
		fmt.Fprintf(b, "| `%s` | %s |\n", d.Decision, src)
	}
	if len(rep.Untraced) > 0 {
		fmt.Fprintf(b, "\n**Untraced decisions (no `@source`):** %s\n", strings.Join(rep.Untraced, ", "))
	}
	if len(rep.Spans) > 0 {
		fmt.Fprintf(b, "\n### Source coverage (heuristic — advisory)\n\n%d/%d source spans referenced by a rule.\n\n",
			rep.Coverage.SpansCovered, rep.Coverage.SpansTotal)
		b.WriteString("| Line | Covered | By | Span |\n|---|---|---|---|\n")
		for _, sp := range rep.Spans {
			mark := "—"
			if sp.Covered {
				mark = "✓"
			}
			span := strings.ReplaceAll(sp.Span, "|", "\\|")
			fmt.Fprintf(b, "| %d | %s | %s | %s |\n", sp.Line, mark, strings.Join(sp.By, ", "), span)
		}
	}
	return nil
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

func mdDomain(d ir.Domain) string {
	switch d.Kind {
	case ir.DomNumeric:
		lo, hi := "-inf", "+inf"
		if !d.LoInf && d.Lo.Num != nil {
			lo = d.Lo.Num.Text('f')
		}
		if !d.HiInf && d.Hi.Num != nil {
			hi = d.Hi.Num.Text('f')
		}
		lb, rb := "[", "]"
		if d.LoOpen {
			lb = "("
		}
		if d.HiOpen {
			rb = ")"
		}
		return lb + lo + ".." + hi + rb
	case ir.DomEnum:
		parts := make([]string, len(d.Enum))
		for i, v := range d.Enum {
			parts[i] = fmt.Sprint(v.ToAny())
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return ""
}

func hitPolicyDoc(h ir.HitPolicy) string {
	switch h {
	case ir.HitUnique:
		return "unique"
	case ir.HitAny:
		return "any"
	case ir.HitFirst:
		return "first"
	case ir.HitPriority:
		return "priority"
	case ir.HitCollect:
		return "collect"
	case ir.HitRuleOrder:
		return "rule order"
	}
	return ""
}

// cmdGraph renders the decision requirements graph (DRG) with verification findings overlaid.
func cmdGraph(args []string) error {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules|.ir.bin file")
	format := fs.String("format", "mermaid", "output format: mermaid|dot|json")
	out := fs.String("o", "", "output file (stdout if absent)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		return fmt.Errorf("--rules is required")
	}
	cm, rep, err := loadModel(*rulesPath, false)
	if err != nil {
		return err
	}
	g := graph.Build(cm, rep)
	var s string
	switch *format {
	case "mermaid":
		s = g.Mermaid()
	case "dot":
		s = g.DOT()
	case "json":
		s = g.JSON()
	default:
		return fmt.Errorf("unknown --format %q (use mermaid|dot|json)", *format)
	}
	if *out == "" {
		fmt.Println(s)
		return nil
	}
	return os.WriteFile(*out, []byte(s), 0o644)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	rulesPath := fs.String("rules", "", "path to the .rules file to serve")
	projectPath := fs.String("project", "", "path to a project directory (or .rules file) to serve")
	addr := fs.String("addr", ":8080", "HTTP listen address")
	watch := fs.Bool("watch", false, "hot reload on file modification")
	strict := fs.Bool("strict", false, "refuse (re)loading if verification has blockers")
	ui := fs.Bool("ui", false, "serve the embedded AI authoring UI at /")
	allowEdit := fs.Bool("allow-edit", false, "enable the project module-editing endpoints (PUT/POST/DELETE /v1/modules write to disk); trusted/loopback hosts only")
	authToken := fs.String("auth-token", "", "require this bearer token on the API (or set FEELC_AUTH_TOKEN); empty = open (default)")
	rateLimit := fs.Int("rate-limit", 0, "max requests/second per client IP (0 = unlimited)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath != "" && *projectPath != "" {
		return fmt.Errorf("--rules and --project are mutually exclusive")
	}
	if *rulesPath == "" && *projectPath == "" && !*ui {
		return fmt.Errorf("--rules or --project is required (or pass --ui to start empty and author in the browser)")
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

	// srv and ws are referenced by the reload closure, so declare them before it.
	var srv *service.Server
	var ws *project.Workspace

	logProject := func(p *project.Project) {
		fmt.Fprintf(os.Stderr, "project %q loaded (%s) — %d module(s), %d blocker(s)\n",
			p.Manifest.Name, p.Hash, len(p.Modules), p.Blockers())
	}

	reloadFn := func() error {
		switch {
		case *projectPath != "":
			p, err := ws.Reload()
			if err != nil {
				fmt.Fprintln(os.Stderr, "reload refused (current project kept):", err)
				return err
			}
			srv.PublishProject(p)
			logProject(p)
			return nil
		case *rulesPath != "":
			e, rep, err := loader.Reload(*rulesPath, reg, *strict)
			logReload(e, rep, err)
			return err
		default:
			return fmt.Errorf("nothing to reload")
		}
	}
	srv = service.New(reg, audit.New(os.Stderr), reloadFn)
	srv.EnableUI = *ui
	// Opt-in hardening for exposed deployments (default off = the local/loopback behavior).
	tok := *authToken
	if tok == "" {
		tok = os.Getenv("FEELC_AUTH_TOKEN")
	}
	srv.SetAuthToken(tok)
	srv.SetRateLimit(*rateLimit)

	// Initial load: must succeed when a file/project is given. With --ui and neither, start empty and
	// let the user author a model in the browser (the model-backed endpoints stay 503 until then).
	modelName := ""
	switch {
	case *projectPath != "":
		w, err := project.OpenWorkspace(*projectPath, *strict)
		if err != nil {
			return fmt.Errorf("initial load: %w", err)
		}
		ws = w
		// The workspace always backs --watch/reload, but the HTTP write endpoints are enabled ONLY with
		// --allow-edit (otherwise PUT/POST/DELETE /v1/modules stay 404). Safe-by-default for exposed hosts.
		if *allowEdit {
			srv.SetWorkspace(ws)
		}
		srv.PublishProject(ws.Current())
		logProject(ws.Current())
		modelName = ws.Current().Manifest.Name
		if *watch {
			stop, err := ws.Watch(func(p *project.Project, err error) {
				if err != nil {
					fmt.Fprintln(os.Stderr, "reload refused (current project kept):", err)
					return
				}
				srv.PublishProject(p)
				logProject(p)
			})
			if err != nil {
				return err
			}
			defer stop()
		}
	case *rulesPath != "":
		e, rep, err := loader.Reload(*rulesPath, reg, *strict)
		if err != nil {
			return fmt.Errorf("initial load: %w", err)
		}
		logReload(e, rep, nil)
		modelName = e.Model.Name
		if *watch {
			stop, err := loader.Watch(*rulesPath, reg, *strict, logReload)
			if err != nil {
				return err
			}
			defer stop()
		}
	}

	if modelName != "" {
		fmt.Fprintf(os.Stderr, "feelc serve on %s (model %q)\n", *addr, modelName)
	} else {
		fmt.Fprintf(os.Stderr, "feelc serve on %s (no model loaded — author one in the UI)\n", *addr)
	}
	if *ui {
		fmt.Fprintf(os.Stderr, "  UI:  %s\n", uiURL(*addr))
		fmt.Fprintf(os.Stderr, "  LLM: %s\n", llmStatusLine())
	}
	if *projectPath != "" {
		if *allowEdit {
			fmt.Fprintln(os.Stderr, "  edit: ON (PUT/POST/DELETE /v1/modules write to disk) — keep this on trusted/loopback hosts only")
		} else {
			fmt.Fprintln(os.Stderr, "  edit: off (read-only; pass --allow-edit to enable module writes)")
		}
	}
	if srv.AuthEnabled() {
		fmt.Fprintln(os.Stderr, "  auth: bearer token required on the API")
	}
	if srv.RateLimited() {
		fmt.Fprintf(os.Stderr, "  rate-limit: %d req/s per client IP\n", *rateLimit)
	}
	// Explicit http.Server with timeouts: the bare ListenAndServe has no read/idle deadlines, leaving the
	// (unauthenticated) endpoints open to slowloris and connection exhaustion.
	server := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second, // LLM proxy calls can be slow
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// Graceful shutdown: on SIGINT/SIGTERM drain in-flight requests (and stop the watchers via the
	// deferred stop()) instead of dropping connections — important for a long-lived rule service.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err // the listener failed to start (e.g. address in use)
	case <-ctx.Done():
		stop() // restore default handling so a second signal force-quits
		fmt.Fprintln(os.Stderr, "shutting down (draining in-flight requests)…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

// cmdHealthcheck probes the local /readyz endpoint and exits 0 (ready) or non-zero otherwise. It is the
// Docker HEALTHCHECK for the distroless image, which has no shell or curl: the same static binary performs
// the probe. --addr matches `feelc serve --addr` (default :8080); a bare (":8080") or all-interfaces
// (0.0.0.0 / [::]) address is probed over loopback. /readyz is auth/rate-limit-exempt, so no token is
// needed; it returns 503 until a model is loaded, which is the correct readiness signal.
func cmdHealthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "serve address to probe (matches `feelc serve --addr`)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("--addr %q: %w", *addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1" // bound to all interfaces (or just ":port") — probe loopback
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/readyz")
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: /readyz returned %d", resp.StatusCode)
	}
	return nil
}

// uiURL renders a clickable URL for a net/http listen address (":8080" -> localhost).
func uiURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr + "/"
	}
	return "http://" + addr + "/"
}

// llmStatusLine reports whether a default LLM key is available in the environment (otherwise the
// user configures their LLM in the UI). The key itself is never printed.
func llmStatusLine() string {
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("FEELC_LLM_API_KEY") != "" {
		return "default API key found in env (override per-request in ⚙ settings)"
	}
	return "no env key — configure your LLM in the UI (⚙ LLM settings)"
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
	unit := cm.Units[*decision]
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		out := map[string]any{"decision": *decision, "output": display(out)}
		if unit != "" {
			out["unit"] = unit
		}
		return enc.Encode(out)
	}
	if unit != "" {
		fmt.Printf("%v %s\n", display(out), unit)
		return nil
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

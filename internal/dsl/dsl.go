// Package dsl parses the feelc .rules source language (the SOURCE OF TRUTH) into a
// *model.Model. In Slice 1 the DSL is deliberately minimal and line-oriented:
//
//	model "name" {}
//	input <name> : <type>
//	decision <name> : <type> {
//	  needs: <a>, <b>
//	  hit: first
//	  <cell> | <cell> => <output> | <output>
//	}
//
// Input/output cells are delegated to the FEEL parser (pbinitiative/feel, cf. ADR 0001),
// except "-" (any) which is handled here. Any construct outside the v1 subset fails outright
// (anti-scope-creep discipline: the parser refuses rather than accept-then-misinterpret).
//
// Errors: every failure site produces a positioned *diag.Error (1-based line + column,
// computed when splitting cells) with a stable code and, when useful, a suggestion.
// ParseFile propagates the file name; Parse delegates with file="" (historical text compat).
package dsl

import (
	"fmt"
	"strings"

	apd "github.com/cockroachdb/apd/v3"
	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/model"
)

// recognized ASCII whitespace (equivalent to unicode.IsSpace for ASCII) for offset computation.
const wsCutset = " \t\v\f\r\n"

// Parse reads a complete .rules source (without a file name; errors formatted as "line N:").
func Parse(src string) (*model.Model, error) {
	return ParseFile("", src)
}

// ParseFile reads a .rules source associating a file name (propagated onto errors).
func ParseFile(file, src string) (*model.Model, error) {
	p := &parser{lines: splitLines(src), file: file}
	m, err := p.parse()
	if err != nil {
		return nil, diag.WithFileIfDiag(err, file)
	}
	return m, nil
}

type line struct {
	text string // content without comment, not trimmed
	no   int    // 1-based line number
}

type parser struct {
	lines   []line
	i       int
	file    string
	pending model.Meta // annotations (`@title`, …) awaiting the next input/decision
}

// takeMeta returns the accumulated annotations and clears them (consumed by an input/decision).
func (p *parser) takeMeta() model.Meta {
	m := p.pending
	p.pending = model.Meta{}
	return m
}

// cellSeg: a trimmed cell and its 1-based column in the source line.
type cellSeg struct {
	text string
	col  int
}

func splitLines(src string) []line {
	raw := strings.Split(src, "\n")
	out := make([]line, len(raw))
	for i, t := range raw {
		out[i] = line{text: stripComment(t), no: i + 1}
	}
	return out
}

// stripComment removes a '#...' comment outside of a string.
func stripComment(s string) string {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inStr = !inStr
		case '#':
			if !inStr {
				return s[:i]
			}
		}
	}
	return s
}

// indentOf returns the number of leading whitespace characters of the raw line
// (0-based offset where the trimmed content begins).
func indentOf(raw string) int {
	return len(raw) - len(strings.TrimLeft(raw, wsCutset))
}

func (p *parser) parse() (*model.Model, error) {
	m := &model.Model{}
	for p.i < len(p.lines) {
		ln := p.lines[p.i]
		t := strings.TrimSpace(ln.text)
		if t == "" {
			p.i++
			continue
		}
		indent := indentOf(ln.text)
		switch {
		case strings.HasPrefix(t, "@"):
			if err := p.parseAnnotation(t, ln.no); err != nil {
				return nil, err
			}
			p.i++
		case strings.HasPrefix(t, "model "):
			if err := p.noDanglingMeta(ln.no); err != nil {
				return nil, err
			}
			name, err := parseModelHeader(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.Name = name
			p.i++
			// if the line is not self-closed (no '}'), consume the block until '}'
			if !strings.Contains(t, "}") {
				p.skipBlock()
			}
		case strings.HasPrefix(t, "input "):
			in, err := parseInput(t, ln.no)
			if err != nil {
				return nil, err
			}
			in.Meta = p.takeMeta()
			m.Inputs = append(m.Inputs, in)
			p.i++
		case strings.HasPrefix(t, "type "):
			if err := p.noDanglingMeta(ln.no); err != nil {
				return nil, err
			}
			td, err := parseTypeDecl(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.Types = append(m.Types, td)
			p.i++
		case strings.HasPrefix(t, "bkm "):
			if err := p.noDanglingMeta(ln.no); err != nil {
				return nil, err
			}
			b, err := parseBKM(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.BKMs = append(m.BKMs, b)
			p.i++
		case strings.HasPrefix(t, "decision "):
			if strings.Contains(t, "{") {
				dec, err := p.parseDecision(t, ln.no, indent) // table (advances p.i)
				if err != nil {
					return nil, err
				}
				dec.Meta = p.takeMeta()
				m.Decisions = append(m.Decisions, dec)
				continue
			}
			dec, err := parseExprDecision(t, ln.no) // literal-expression (1 line)
			if err != nil {
				return nil, err
			}
			dec.Meta = p.takeMeta()
			m.Decisions = append(m.Decisions, dec)
			p.i++
		default:
			return nil, diag.Newf(diag.CodeUnknownStmt, ln.no, "unrecognized statement: %q", t).
				WithSuggestion("valid statements: `model`, `input`, `type`, `decision`")
		}
	}
	if err := p.noDanglingMeta(len(p.lines)); err != nil {
		return nil, err
	}
	if m.Name == "" {
		return nil, diag.New(diag.CodeNoModel, 0, `model without a `+"`model \"...\"`"+` declaration`).
			WithSuggestion("add a `model \"name\" {}` line at the top of the file")
	}
	return m, nil
}

// parseAnnotation parses a `@key "value"` documentation line into the pending Meta.
func (p *parser) parseAnnotation(t string, no int) error {
	rest := strings.TrimSpace(t[1:])
	key := rest
	if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
		key = rest[:sp]
	}
	val, ok := firstQuoted(rest)
	if !ok {
		return diag.Newf(diag.CodeUnknownStmt, no, "annotation @%s expects a quoted value", key).
			WithSuggestion(`example: @title "Eligibility"`)
	}
	switch key {
	case "title":
		p.pending.Title = val
	case "doc":
		p.pending.Doc = val
	case "question":
		p.pending.Question = val
	case "source":
		p.pending.Source = val
	default:
		return diag.Newf(diag.CodeUnknownStmt, no, "unknown annotation @%s", key).
			WithSuggestion("supported annotations: @title, @doc, @question, @source")
	}
	return nil
}

// noDanglingMeta errors if annotations were written but not immediately followed by an
// input/decision (they would otherwise silently attach to the wrong statement).
func (p *parser) noDanglingMeta(no int) error {
	if !p.pending.Empty() {
		return diag.New(diag.CodeUnknownStmt, no, "annotation (@…) must immediately precede an input or decision")
	}
	return nil
}

// skipBlock consumes lines until a line containing '}' (inclusive).
func (p *parser) skipBlock() {
	for p.i < len(p.lines) {
		if strings.Contains(p.lines[p.i].text, "}") {
			p.i++
			return
		}
		p.i++
	}
}

func parseModelHeader(t string, no int) (string, error) {
	name, ok := firstQuoted(t)
	if !ok {
		return "", diag.New(diag.CodeModelHeader, no, "`model` expects a quoted name").
			WithSuggestion(`example: model "credit" {}`)
	}
	return name, nil
}

func parseInput(t string, no int) (model.Input, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "input"))
	name, typespec, ok := splitColon(rest)
	if !ok {
		return model.Input{}, diag.New(diag.CodeInputSyntax, no, "`input` expects `name : type`").
			WithSuggestion("example: input credit_score : number")
	}
	// typespec = "<type>" optionally followed by a domain ("number in [300..850]", "number >= 0").
	fields := strings.Fields(typespec)
	if len(fields) == 0 {
		return model.Input{}, diag.New(diag.CodeInputSyntax, no, "missing input type")
	}
	mt, err := parseType(fields[0], no)
	if err != nil {
		return model.Input{}, err
	}
	domain := strings.TrimSpace(strings.TrimPrefix(typespec, fields[0]))
	// Optional trailing `unit "..."` clause (numeric inputs only — the grammar allows units only
	// there, and restricting it avoids the `unit ` substring colliding with a string-enum domain
	// like `in {"unit price", "qty"}`).
	unit := ""
	if mt == model.TypeNumber {
		if idx := strings.Index(domain, "unit "); idx >= 0 {
			u, ok := firstQuoted(domain[idx:])
			if !ok {
				return model.Input{}, diag.New(diag.CodeInputSyntax, no, `input unit must be quoted, e.g. unit "EUR/month"`)
			}
			unit = u
			domain = strings.TrimSpace(domain[:idx])
		}
	}
	return model.Input{Name: name, Type: mt, Domain: domain, Unit: unit, Line: no}, nil
}

func (p *parser) parseDecision(header string, no, indent int) (model.Decision, error) {
	dec := model.Decision{Line: no}
	// `decision <name> : <type> {`
	if !strings.Contains(header, "{") {
		return dec, diag.New(diag.CodeDecisionHead, no, "missing `{` at end of decision header")
	}
	if idx := strings.IndexByte(header, '{'); strings.TrimSpace(header[idx+1:]) != "" {
		return dec, diag.New(diag.CodeBraceTrailing, no,
			"place `{` at end of line; the decision body goes on the following lines")
	}
	h := strings.TrimSpace(strings.TrimPrefix(header, "decision"))
	h = strings.TrimSuffix(strings.TrimSpace(h), "{")
	name, typ, ok := splitColon(h)
	if !ok {
		return dec, diag.New(diag.CodeDecisionHead, no, "expected decision header `decision name : type {`")
	}
	dec.Name = strings.TrimSpace(name)
	dec.TypeName = strings.TrimSpace(typ) // builtin or declared context type: resolved by the compiler
	p.i++                                 // consume the header (the trailing `{` was already validated above)

	for p.i < len(p.lines) {
		ln := p.lines[p.i]
		t := strings.TrimSpace(ln.text)
		if t == "" {
			p.i++
			continue
		}
		lineIndent := indentOf(ln.text)
		if t == "}" {
			p.i++
			return dec, nil
		}
		switch {
		case strings.HasPrefix(t, "needs:"):
			dec.Needs = splitList(strings.TrimPrefix(t, "needs:"))
		case strings.HasPrefix(t, "bracket:"):
			dec.Bracket = strings.TrimSpace(strings.TrimPrefix(t, "bracket:"))
			if dec.Bracket == "" {
				return dec, diag.New(diag.CodeDecisionBody, ln.no, "`bracket:` requires an input name, e.g. `bracket: income`")
			}
		case strings.HasPrefix(t, "= "):
			// expression decision in block form (so it can carry `applicable if`)
			cell, err := parseCell(strings.TrimSpace(t[1:]), ln.no, lineIndent+2, true)
			if err != nil {
				return dec, err
			}
			dec.Expr = &cell
		case strings.HasPrefix(t, "not applicable if "):
			cell, err := parseCell(strings.TrimSpace(strings.TrimPrefix(t, "not applicable if")), ln.no, lineIndent+18, true)
			if err != nil {
				return dec, err
			}
			dec.Applicable, dec.ApplicableNeg = &cell, true
		case strings.HasPrefix(t, "applicable if "):
			cell, err := parseCell(strings.TrimSpace(strings.TrimPrefix(t, "applicable if")), ln.no, lineIndent+14, true)
			if err != nil {
				return dec, err
			}
			dec.Applicable, dec.ApplicableNeg = &cell, false
		case strings.HasPrefix(t, "hit:"):
			dec.HitPolicy = strings.TrimSpace(strings.TrimPrefix(t, "hit:"))
		case strings.HasPrefix(t, "priority:"):
			for _, c := range splitListCol(strings.TrimPrefix(t, "priority:"), len("priority:"), lineIndent) {
				cell, err := parseCell(c.text, ln.no, c.col, true)
				if err != nil {
					return dec, err
				}
				dec.Priority = append(dec.Priority, cell)
			}
		case strings.Contains(t, "=>"):
			r, err := parseRule(t, ln.no, lineIndent)
			if err != nil {
				return dec, err
			}
			dec.Rules = append(dec.Rules, r)
		default:
			return dec, diag.Newf(diag.CodeDecisionBody, ln.no, "unrecognized decision line: %q", t)
		}
		p.i++
	}
	return dec, diag.Newf(diag.CodeDecisionBody, no, "missing `}` for decision %q", dec.Name)
}

func parseRule(t string, no, indent int) (model.Rule, error) {
	idx := strings.Index(t, "=>")
	if idx < 0 {
		return model.Rule{}, diag.New(diag.CodeRuleSyntax, no, "rule without `=>`")
	}
	lhs := t[:idx]
	rhs := t[idx+2:]
	r := model.Rule{Line: no}
	condCells := splitCellsCol(lhs, 0, indent)
	if isDefaultLHS(condCells) {
		r.IsDefault = true // `default` line: no conditions
	} else {
		for _, c := range condCells {
			cell, err := parseCell(c.text, no, c.col, false)
			if err != nil {
				return model.Rule{}, err
			}
			r.Conds = append(r.Conds, cell)
		}
	}
	for _, c := range splitCellsCol(rhs, idx+2, indent) {
		cell, err := parseCell(c.text, no, c.col, true)
		if err != nil {
			return model.Rule{}, err
		}
		r.Outputs = append(r.Outputs, cell)
	}
	return r, nil
}

// isDefaultLHS recognizes a `default` line (its other cells are only empty alignment).
func isDefaultLHS(cells []cellSeg) bool {
	if len(cells) == 0 || cells[0].text != "default" {
		return false
	}
	for _, c := range cells[1:] {
		if c.text != "" {
			return false
		}
	}
	return true
}

// parseExprDecision parses `decision <name> : <type> = <FEEL expr>` (literal-expression decision).
func parseExprDecision(t string, no int) (model.Decision, error) {
	h := strings.TrimSpace(strings.TrimPrefix(t, "decision"))
	name, rest, ok := splitColon(h)
	if !ok {
		return model.Decision{}, diag.New(diag.CodeDecisionHead, no, "decision: expected `name : type ...`")
	}
	typeName, exprSrc, ok := strings.Cut(rest, "=")
	if !ok {
		return model.Decision{}, diag.Newf(diag.CodeDecisionHead, no,
			"decision %q: expected `{` (table) or `= expression`", name)
	}
	exprSrc = strings.TrimSpace(exprSrc)
	node, err := feel.ParseString(exprSrc)
	if err != nil {
		return model.Decision{}, diag.Wrap(diag.CodeFeelSyntax, no,
			fmt.Sprintf("invalid FEEL expression %q", exprSrc), err)
	}
	return model.Decision{
		Name:     name,
		TypeName: strings.TrimSpace(typeName),
		Expr:     &model.Cell{Src: exprSrc, Node: node, Line: no},
		Line:     no,
	}, nil
}

// parseBKM parses `bkm <name>(p1:t1, p2:t2):ret = <FEEL expr>`.
// The `(p:t):ret` signature is feelc DSL syntax (not standard FEEL) — split here;
// only the `= expr` body is delegated to the FEEL parser.
func parseBKM(t string, no int) (model.BKM, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "bkm"))
	op := strings.IndexByte(rest, '(')
	cp := strings.IndexByte(rest, ')')
	if op < 0 || cp < 0 || cp < op {
		return model.BKM{}, diag.New(diag.CodeBKM, no,
			"`bkm` expects `name(p1:t1, ...):type = expression`").
			WithSuggestion("example: bkm dti(debt:number, income:number):number = debt / (income / 12)")
	}
	name := strings.TrimSpace(rest[:op])
	if name == "" {
		return model.BKM{}, diag.New(diag.CodeBKM, no, "missing BKM name before `(`")
	}
	bkm := model.BKM{Name: name, Line: no}
	for _, p := range splitList(rest[op+1 : cp]) {
		pn, pt, ok := splitColon(p)
		if !ok {
			return model.BKM{}, diag.Newf(diag.CodeBKM, no, "BKM parameter: expected `name: type`, got %q", p)
		}
		mt, err := parseType(pt, no)
		if err != nil {
			return model.BKM{}, err
		}
		bkm.Params = append(bkm.Params, model.Field{Name: pn, Type: mt})
	}
	after := strings.TrimSpace(rest[cp+1:])
	if !strings.HasPrefix(after, ":") {
		return model.BKM{}, diag.New(diag.CodeBKM, no, "expected return type after `)`: `):type = expr`")
	}
	retName, bodySrc, ok := strings.Cut(after[1:], "=")
	if !ok {
		return model.BKM{}, diag.New(diag.CodeBKM, no, "expected BKM body: `= expression`")
	}
	ret, err := parseType(strings.TrimSpace(retName), no)
	if err != nil {
		return model.BKM{}, err
	}
	bkm.Ret = ret
	bodySrc = strings.TrimSpace(bodySrc)
	node, err := feel.ParseString(bodySrc)
	if err != nil {
		return model.BKM{}, diag.Wrap(diag.CodeFeelSyntax, no,
			fmt.Sprintf("invalid FEEL BKM body %q", bodySrc), err)
	}
	bkm.Body = &model.Cell{Src: bodySrc, Node: node, Line: no}
	return bkm, nil
}

// parseTypeDecl parses `type <Name> = context { f1: t1, f2: t2 }` (on one line in v2).
func parseTypeDecl(t string, no int) (model.TypeDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "type"))
	name, rhs, ok := strings.Cut(rest, "=")
	if !ok {
		return model.TypeDecl{}, diag.New(diag.CodeTypeDecl, no, "expected `type Name = context { ... }`")
	}
	rhs = strings.TrimSpace(rhs)
	if !strings.HasPrefix(rhs, "context") {
		return model.TypeDecl{}, diag.New(diag.CodeTypeDecl, no,
			"only `context { ... }` types are supported in v2")
	}
	open := strings.IndexByte(rhs, '{')
	closeB := strings.LastIndexByte(rhs, '}')
	if open < 0 || closeB < 0 || closeB < open {
		return model.TypeDecl{}, diag.New(diag.CodeTypeDecl, no, "context type: expected `{ ... }` on one line")
	}
	td := model.TypeDecl{Name: strings.TrimSpace(name), Line: no}
	for _, f := range strings.Split(rhs[open+1:closeB], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		fn, ft, ok := splitColon(f)
		if !ok {
			return model.TypeDecl{}, diag.Newf(diag.CodeTypeDecl, no,
				"context field: expected `name: type`, got %q", f)
		}
		mt, err := parseType(ft, no)
		if err != nil {
			return model.TypeDecl{}, err
		}
		td.Fields = append(td.Fields, model.Field{Name: fn, Type: mt})
	}
	return td, nil
}

// parseCell normalizes a cell. isOutput=false for a condition, true for an output.
// col is the 1-based column of the cell in the source line (0 if unknown).
func parseCell(src string, no, col int, isOutput bool) (model.Cell, error) {
	s := strings.TrimSpace(src)
	cell := model.Cell{Src: s, Line: no, Col: col}
	if s == "-" && !isOutput {
		cell.Dash = true
		return cell, nil
	}
	if s == "" {
		return model.Cell{}, diag.New(diag.CodeEmptyCell, no, "empty cell").WithCol(col)
	}
	// Percent literal: `30%` is the exact decimal 0.30 (Publicodes-style; useful for bracket rates).
	if frac, ok := percentLiteral(s); ok {
		cell.Node = &feel.NumberNode{Value: frac}
		return cell, nil
	}
	node, err := feel.ParseString(s)
	if err != nil {
		return model.Cell{}, diag.Wrap(diag.CodeFeelSyntax, no,
			fmt.Sprintf("invalid FEEL cell %q", s), err).WithCol(col)
	}
	cell.Node = node
	return cell, nil
}

// percentLiteral converts a whole-cell percent literal (e.g. "30%", "2.5%") into its exact decimal
// fraction string ("0.30", "0.025"). Returns ok=false if the cell is not a bare percentage.
func percentLiteral(s string) (string, bool) {
	if !strings.HasSuffix(s, "%") {
		return "", false
	}
	num := strings.TrimSpace(s[:len(s)-1])
	d, err := decimal.Parse(num)
	if err != nil || d.Form != apd.Finite {
		return "", false // reject "Inf%"/"NaN%": never inject a non-finite constant
	}
	res := decimal.FromInt(0)
	if _, err := decimal.Ctx.Quo(res, d, decimal.FromInt(100)); err != nil {
		return "", false
	}
	res.Reduce(res) // canonical, compact form: 30% -> "0.3" (matches the hand-written literal)
	return res.Text('f'), true
}

// --- splitting helpers ---

// splitCellsCol splits s by '|' and returns each trimmed cell with its 1-based
// column in the source line. baseOff = 0-based offset of s in the trimmed line;
// indent = number of leading whitespace characters of the raw line.
func splitCellsCol(s string, baseOff, indent int) []cellSeg {
	var out []cellSeg
	start := 0
	for {
		rel := strings.IndexByte(s[start:], '|')
		end := len(s)
		if rel >= 0 {
			end = start + rel
		}
		part := s[start:end]
		leading := len(part) - len(strings.TrimLeft(part, wsCutset))
		out = append(out, cellSeg{
			text: strings.TrimSpace(part),
			col:  indent + baseOff + start + leading + 1,
		})
		if rel < 0 {
			break
		}
		start = end + 1
	}
	return out
}

// splitListCol splits s by ',' (ignoring empty segments) with 1-based columns.
func splitListCol(s string, baseOff, indent int) []cellSeg {
	var out []cellSeg
	start := 0
	for {
		rel := strings.IndexByte(s[start:], ',')
		end := len(s)
		if rel >= 0 {
			end = start + rel
		}
		part := s[start:end]
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			leading := len(part) - len(strings.TrimLeft(part, wsCutset))
			out = append(out, cellSeg{text: trimmed, col: indent + baseOff + start + leading + 1})
		}
		if rel < 0 {
			break
		}
		start = end + 1
	}
	return out
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitColon(s string) (left, right string, ok bool) {
	l, r, found := strings.Cut(s, ":")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(l), strings.TrimSpace(r), true
}

func firstQuoted(s string) (string, bool) {
	a := strings.IndexByte(s, '"')
	if a < 0 {
		return "", false
	}
	b := strings.IndexByte(s[a+1:], '"')
	if b < 0 {
		return "", false
	}
	return s[a+1 : a+1+b], true
}

func parseType(s string, no int) (model.Type, error) {
	switch strings.TrimSpace(s) {
	case "number":
		return model.TypeNumber, nil
	case "string":
		return model.TypeString, nil
	case "boolean", "bool":
		return model.TypeBool, nil
	case "date":
		return model.TypeDate, nil
	case "duration":
		return model.TypeDuration, nil
	default:
		return "", diag.Newf(diag.CodeUnknownType, no, "type not supported: %q", s).
			WithSuggestion("supported types: number, string, boolean, date, duration")
	}
}

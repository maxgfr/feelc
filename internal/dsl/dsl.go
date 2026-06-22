// Package dsl parse le langage source .rules de feelc (la SOURCE DE VÉRITÉ) vers un
// *model.Model. En Tranche 1 le DSL est volontairement minimal et orienté lignes :
//
//	model "name" {}
//	input <name> : <type>
//	decision <name> : <type> {
//	  needs: <a>, <b>
//	  hit: first
//	  <cellule> | <cellule> => <sortie> | <sortie>
//	}
//
// Les cellules d'entrée/sortie sont confiées au parseur FEEL (pbinitiative/feel, cf. ADR 0001),
// sauf "-" (any) traité ici. Tout construct hors sous-ensemble v1 échoue franchement
// (discipline anti-scope-creep : le parseur refuse plutôt que d'accepter-puis-mal-interpréter).
//
// Erreurs : tout site d'échec produit un *diag.Error positionné (ligne + colonne 1-based,
// calculée au split des cellules) avec un code stable et, quand utile, une suggestion.
// ParseFile propage le nom de fichier ; Parse délègue avec file="" (compat texte historique).
package dsl

import (
	"fmt"
	"strings"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/model"
)

// whitespace ASCII reconnu (équivalent unicode.IsSpace pour l'ASCII) pour le calcul d'offsets.
const wsCutset = " \t\v\f\r\n"

// Parse lit une source .rules complète (sans nom de fichier ; erreurs au format "ligne N:").
func Parse(src string) (*model.Model, error) {
	return ParseFile("", src)
}

// ParseFile lit une source .rules en associant un nom de fichier (propagé sur les erreurs).
func ParseFile(file, src string) (*model.Model, error) {
	p := &parser{lines: splitLines(src), file: file}
	m, err := p.parse()
	if err != nil {
		return nil, diag.WithFileIfDiag(err, file)
	}
	return m, nil
}

type line struct {
	text string // contenu sans commentaire, non trimé
	no   int    // numéro de ligne 1-based
}

type parser struct {
	lines []line
	i     int
	file  string
}

// cellSeg : une cellule trimée et sa colonne 1-based dans la ligne source.
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

// stripComment retire un commentaire '#...' hors chaîne de caractères.
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

// indentOf renvoie le nombre de caractères blancs en tête de la ligne brute
// (offset 0-based où commence le contenu trimé).
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
		case strings.HasPrefix(t, "model "):
			name, err := parseModelHeader(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.Name = name
			p.i++
			// si la ligne n'est pas auto-fermée (pas de '}'), consommer le bloc jusqu'à '}'
			if !strings.Contains(t, "}") {
				p.skipBlock()
			}
		case strings.HasPrefix(t, "input "):
			in, err := parseInput(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.Inputs = append(m.Inputs, in)
			p.i++
		case strings.HasPrefix(t, "type "):
			td, err := parseTypeDecl(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.Types = append(m.Types, td)
			p.i++
		case strings.HasPrefix(t, "bkm "):
			b, err := parseBKM(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.BKMs = append(m.BKMs, b)
			p.i++
		case strings.HasPrefix(t, "decision "):
			if strings.Contains(t, "{") {
				dec, err := p.parseDecision(t, ln.no, indent) // table (avance p.i)
				if err != nil {
					return nil, err
				}
				m.Decisions = append(m.Decisions, dec)
				continue
			}
			dec, err := parseExprDecision(t, ln.no) // literal-expression (1 ligne)
			if err != nil {
				return nil, err
			}
			m.Decisions = append(m.Decisions, dec)
			p.i++
		default:
			return nil, diag.Newf(diag.CodeUnknownStmt, ln.no, "instruction non reconnue: %q", t).
				WithSuggestion("instructions valides : `model`, `input`, `type`, `decision`")
		}
	}
	if m.Name == "" {
		return nil, diag.New(diag.CodeNoModel, 0, `modèle sans déclaration `+"`model \"...\"`").
			WithSuggestion("ajoutez une ligne `model \"nom\" {}` en tête du fichier")
	}
	return m, nil
}

// skipBlock consomme les lignes jusqu'à une ligne contenant '}' (incluse).
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
		return "", diag.New(diag.CodeModelHeader, no, "`model` attend un nom entre guillemets").
			WithSuggestion(`exemple : model "credit" {}`)
	}
	return name, nil
}

func parseInput(t string, no int) (model.Input, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "input"))
	name, typespec, ok := splitColon(rest)
	if !ok {
		return model.Input{}, diag.New(diag.CodeInputSyntax, no, "`input` attend `nom : type`").
			WithSuggestion("exemple : input credit_score : number")
	}
	// typespec = "<type>" éventuellement suivi d'un domaine ("number in [300..850]", "number >= 0").
	fields := strings.Fields(typespec)
	if len(fields) == 0 {
		return model.Input{}, diag.New(diag.CodeInputSyntax, no, "type d'entrée manquant")
	}
	mt, err := parseType(fields[0], no)
	if err != nil {
		return model.Input{}, err
	}
	domain := strings.TrimSpace(strings.TrimPrefix(typespec, fields[0]))
	return model.Input{Name: name, Type: mt, Domain: domain, Line: no}, nil
}

func (p *parser) parseDecision(header string, no, indent int) (model.Decision, error) {
	dec := model.Decision{Line: no}
	// `decision <name> : <type> {`
	if !strings.Contains(header, "{") {
		return dec, diag.New(diag.CodeDecisionHead, no, "`{` manquant en fin d'en-tête de décision")
	}
	if idx := strings.IndexByte(header, '{'); strings.TrimSpace(header[idx+1:]) != "" {
		return dec, diag.New(diag.CodeBraceTrailing, no,
			"placez `{` en fin de ligne ; le corps de la décision va sur les lignes suivantes")
	}
	h := strings.TrimSpace(strings.TrimPrefix(header, "decision"))
	h = strings.TrimSuffix(strings.TrimSpace(h), "{")
	name, typ, ok := splitColon(h)
	if !ok {
		return dec, diag.New(diag.CodeDecisionHead, no, "en-tête de décision attendu `decision nom : type {`")
	}
	dec.Name = strings.TrimSpace(name)
	dec.TypeName = strings.TrimSpace(typ) // builtin ou type context déclaré : résolu par le compilateur
	p.i++                                 // consommer l'en-tête (le `{` en fin de ligne a déjà été validé plus haut)

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
			return dec, diag.Newf(diag.CodeDecisionBody, ln.no, "ligne de décision non reconnue: %q", t)
		}
		p.i++
	}
	return dec, diag.Newf(diag.CodeDecisionBody, no, "`}` manquant pour la décision %q", dec.Name)
}

func parseRule(t string, no, indent int) (model.Rule, error) {
	idx := strings.Index(t, "=>")
	if idx < 0 {
		return model.Rule{}, diag.New(diag.CodeRuleSyntax, no, "règle sans `=>`")
	}
	lhs := t[:idx]
	rhs := t[idx+2:]
	r := model.Rule{Line: no}
	condCells := splitCellsCol(lhs, 0, indent)
	if isDefaultLHS(condCells) {
		r.IsDefault = true // ligne `default` : pas de conditions
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

// isDefaultLHS reconnaît une ligne `default` (ses autres cellules ne sont que de l'alignement vide).
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

// parseExprDecision parse `decision <name> : <type> = <expr FEEL>` (décision literal-expression).
func parseExprDecision(t string, no int) (model.Decision, error) {
	h := strings.TrimSpace(strings.TrimPrefix(t, "decision"))
	name, rest, ok := splitColon(h)
	if !ok {
		return model.Decision{}, diag.New(diag.CodeDecisionHead, no, "décision: `nom : type ...` attendu")
	}
	typeName, exprSrc, ok := strings.Cut(rest, "=")
	if !ok {
		return model.Decision{}, diag.Newf(diag.CodeDecisionHead, no,
			"décision %q: attendu `{` (table) ou `= expression`", name)
	}
	exprSrc = strings.TrimSpace(exprSrc)
	node, err := feel.ParseString(exprSrc)
	if err != nil {
		return model.Decision{}, diag.Wrap(diag.CodeFeelSyntax, no,
			fmt.Sprintf("expression FEEL invalide %q", exprSrc), err)
	}
	return model.Decision{
		Name:     name,
		TypeName: strings.TrimSpace(typeName),
		Expr:     &model.Cell{Src: exprSrc, Node: node, Line: no},
		Line:     no,
	}, nil
}

// parseBKM parse `bkm <name>(p1:t1, p2:t2):ret = <expr FEEL>`.
// La signature `(p:t):ret` est de la syntaxe DSL feelc (pas du FEEL standard) — découpée ici ;
// seul le corps `= expr` est confié au parseur FEEL.
func parseBKM(t string, no int) (model.BKM, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "bkm"))
	op := strings.IndexByte(rest, '(')
	cp := strings.IndexByte(rest, ')')
	if op < 0 || cp < 0 || cp < op {
		return model.BKM{}, diag.New(diag.CodeBKM, no,
			"`bkm` attend `nom(p1:t1, ...):type = expression`").
			WithSuggestion("exemple : bkm dti(debt:number, income:number):number = debt / (income / 12)")
	}
	name := strings.TrimSpace(rest[:op])
	if name == "" {
		return model.BKM{}, diag.New(diag.CodeBKM, no, "nom de BKM manquant avant `(`")
	}
	bkm := model.BKM{Name: name, Line: no}
	for _, p := range splitList(rest[op+1 : cp]) {
		pn, pt, ok := splitColon(p)
		if !ok {
			return model.BKM{}, diag.Newf(diag.CodeBKM, no, "paramètre de BKM: `nom: type` attendu, obtenu %q", p)
		}
		mt, err := parseType(pt, no)
		if err != nil {
			return model.BKM{}, err
		}
		bkm.Params = append(bkm.Params, model.Field{Name: pn, Type: mt})
	}
	after := strings.TrimSpace(rest[cp+1:])
	if !strings.HasPrefix(after, ":") {
		return model.BKM{}, diag.New(diag.CodeBKM, no, "type de retour attendu après `)` : `):type = expr`")
	}
	retName, bodySrc, ok := strings.Cut(after[1:], "=")
	if !ok {
		return model.BKM{}, diag.New(diag.CodeBKM, no, "corps de BKM attendu : `= expression`")
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
			fmt.Sprintf("corps de BKM FEEL invalide %q", bodySrc), err)
	}
	bkm.Body = &model.Cell{Src: bodySrc, Node: node, Line: no}
	return bkm, nil
}

// parseTypeDecl parse `type <Name> = context { f1: t1, f2: t2 }` (sur une ligne en v2).
func parseTypeDecl(t string, no int) (model.TypeDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "type"))
	name, rhs, ok := strings.Cut(rest, "=")
	if !ok {
		return model.TypeDecl{}, diag.New(diag.CodeTypeDecl, no, "`type Nom = context { ... }` attendu")
	}
	rhs = strings.TrimSpace(rhs)
	if !strings.HasPrefix(rhs, "context") {
		return model.TypeDecl{}, diag.New(diag.CodeTypeDecl, no,
			"seuls les types `context { ... }` sont supportés en v2")
	}
	open := strings.IndexByte(rhs, '{')
	closeB := strings.LastIndexByte(rhs, '}')
	if open < 0 || closeB < 0 || closeB < open {
		return model.TypeDecl{}, diag.New(diag.CodeTypeDecl, no, "type context : `{ ... }` attendu sur une ligne")
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
				"champ de context: `nom: type` attendu, obtenu %q", f)
		}
		mt, err := parseType(ft, no)
		if err != nil {
			return model.TypeDecl{}, err
		}
		td.Fields = append(td.Fields, model.Field{Name: fn, Type: mt})
	}
	return td, nil
}

// parseCell normalise une cellule. isOutput=false pour une condition, true pour une sortie.
// col est la colonne 1-based de la cellule dans la ligne source (0 si inconnue).
func parseCell(src string, no, col int, isOutput bool) (model.Cell, error) {
	s := strings.TrimSpace(src)
	cell := model.Cell{Src: s, Line: no, Col: col}
	if s == "-" && !isOutput {
		cell.Dash = true
		return cell, nil
	}
	if s == "" {
		return model.Cell{}, diag.New(diag.CodeEmptyCell, no, "cellule vide").WithCol(col)
	}
	node, err := feel.ParseString(s)
	if err != nil {
		return model.Cell{}, diag.Wrap(diag.CodeFeelSyntax, no,
			fmt.Sprintf("cellule FEEL invalide %q", s), err).WithCol(col)
	}
	cell.Node = node
	return cell, nil
}

// --- helpers de découpe ---

// splitCellsCol découpe s par '|' et renvoie chaque cellule trimée avec sa colonne
// 1-based dans la ligne source. baseOff = offset 0-based de s dans la ligne trimée ;
// indent = nombre de blancs en tête de la ligne brute.
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

// splitListCol découpe s par ',' (en ignorant les segments vides) avec colonnes 1-based.
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
	default:
		return "", diag.Newf(diag.CodeUnknownType, no, "type non supporté en v1: %q", s).
			WithSuggestion("types supportés : number, string, boolean")
	}
}

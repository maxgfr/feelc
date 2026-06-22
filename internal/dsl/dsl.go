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
package dsl

import (
	"fmt"
	"strings"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/model"
)

// Parse lit une source .rules complète.
func Parse(src string) (*model.Model, error) {
	p := &parser{lines: splitLines(src)}
	return p.parse()
}

type line struct {
	text string // contenu sans commentaire, non trimé
	no   int    // numéro de ligne 1-based
}

type parser struct {
	lines []line
	i     int
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

func (p *parser) parse() (*model.Model, error) {
	m := &model.Model{}
	for p.i < len(p.lines) {
		ln := p.lines[p.i]
		t := strings.TrimSpace(ln.text)
		if t == "" {
			p.i++
			continue
		}
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
		case strings.HasPrefix(t, "decision "):
			if strings.Contains(t, "{") {
				dec, err := p.parseDecision(t, ln.no) // table (avance p.i)
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
			return nil, fmt.Errorf("ligne %d: instruction non reconnue: %q", ln.no, t)
		}
	}
	if m.Name == "" {
		return nil, fmt.Errorf("modèle sans déclaration `model \"...\"`")
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
		return "", fmt.Errorf("ligne %d: `model` attend un nom entre guillemets", no)
	}
	return name, nil
}

func parseInput(t string, no int) (model.Input, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "input"))
	name, typespec, ok := splitColon(rest)
	if !ok {
		return model.Input{}, fmt.Errorf("ligne %d: `input` attend `nom : type`", no)
	}
	// typespec = "<type>" éventuellement suivi d'un domaine ("number in [300..850]", "number >= 0").
	fields := strings.Fields(typespec)
	if len(fields) == 0 {
		return model.Input{}, fmt.Errorf("ligne %d: type d'entrée manquant", no)
	}
	mt, err := parseType(fields[0], no)
	if err != nil {
		return model.Input{}, err
	}
	domain := strings.TrimSpace(strings.TrimPrefix(typespec, fields[0]))
	return model.Input{Name: name, Type: mt, Domain: domain, Line: no}, nil
}

func (p *parser) parseDecision(header string, no int) (model.Decision, error) {
	dec := model.Decision{Line: no}
	// `decision <name> : <type> {`
	if !strings.Contains(header, "{") {
		return dec, fmt.Errorf("ligne %d: `{` manquant en fin d'en-tête de décision", no)
	}
	if idx := strings.IndexByte(header, '{'); strings.TrimSpace(header[idx+1:]) != "" {
		return dec, fmt.Errorf("ligne %d: placez `{` en fin de ligne ; le corps de la décision va sur les lignes suivantes", no)
	}
	h := strings.TrimSpace(strings.TrimPrefix(header, "decision"))
	h = strings.TrimSuffix(strings.TrimSpace(h), "{")
	name, typ, ok := splitColon(h)
	if !ok {
		return dec, fmt.Errorf("ligne %d: en-tête de décision attendu `decision nom : type {`", no)
	}
	dec.Name = strings.TrimSpace(name)
	dec.TypeName = strings.TrimSpace(typ) // builtin ou type context déclaré : résolu par le compilateur
	p.i++                                  // consommer l'en-tête (le `{` en fin de ligne a déjà été validé plus haut)

	for p.i < len(p.lines) {
		ln := p.lines[p.i]
		t := strings.TrimSpace(ln.text)
		if t == "" {
			p.i++
			continue
		}
		if t == "}" {
			p.i++
			return dec, nil
		}
		switch {
		case strings.HasPrefix(t, "needs:"):
			dec.Needs = splitList(strings.TrimPrefix(t, "needs:"))
		case strings.HasPrefix(t, "hit:"):
			dec.HitPolicy = strings.TrimSpace(strings.TrimPrefix(t, "hit:"))
		case strings.Contains(t, "=>"):
			r, err := parseRule(t, ln.no)
			if err != nil {
				return dec, err
			}
			dec.Rules = append(dec.Rules, r)
		default:
			return dec, fmt.Errorf("ligne %d: ligne de décision non reconnue: %q", ln.no, t)
		}
		p.i++
	}
	return dec, fmt.Errorf("ligne %d: `}` manquant pour la décision %q", no, dec.Name)
}

func parseRule(t string, no int) (model.Rule, error) {
	lhs, rhs, ok := strings.Cut(t, "=>")
	if !ok {
		return model.Rule{}, fmt.Errorf("ligne %d: règle sans `=>`", no)
	}
	r := model.Rule{Line: no}
	condCells := splitCells(lhs)
	if isDefaultLHS(condCells) {
		r.IsDefault = true // ligne `default` : pas de conditions
	} else {
		for _, c := range condCells {
			cell, err := parseCell(c, no, false)
			if err != nil {
				return model.Rule{}, err
			}
			r.Conds = append(r.Conds, cell)
		}
	}
	for _, c := range splitCells(rhs) {
		cell, err := parseCell(c, no, true)
		if err != nil {
			return model.Rule{}, err
		}
		r.Outputs = append(r.Outputs, cell)
	}
	return r, nil
}

// isDefaultLHS reconnaît une ligne `default` (ses autres cellules ne sont que de l'alignement vide).
func isDefaultLHS(cells []string) bool {
	if len(cells) == 0 || cells[0] != "default" {
		return false
	}
	for _, c := range cells[1:] {
		if c != "" {
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
		return model.Decision{}, fmt.Errorf("ligne %d: décision: `nom : type ...` attendu", no)
	}
	typeName, exprSrc, ok := strings.Cut(rest, "=")
	if !ok {
		return model.Decision{}, fmt.Errorf("ligne %d: décision %q: attendu `{` (table) ou `= expression`", no, name)
	}
	exprSrc = strings.TrimSpace(exprSrc)
	node, err := feel.ParseString(exprSrc)
	if err != nil {
		return model.Decision{}, fmt.Errorf("ligne %d: expression FEEL invalide %q: %w", no, exprSrc, err)
	}
	return model.Decision{
		Name:     name,
		TypeName: strings.TrimSpace(typeName),
		Expr:     &model.Cell{Src: exprSrc, Node: node, Line: no},
		Line:     no,
	}, nil
}

// parseTypeDecl parse `type <Name> = context { f1: t1, f2: t2 }` (sur une ligne en v2).
func parseTypeDecl(t string, no int) (model.TypeDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(t, "type"))
	name, rhs, ok := strings.Cut(rest, "=")
	if !ok {
		return model.TypeDecl{}, fmt.Errorf("ligne %d: `type Nom = context { ... }` attendu", no)
	}
	rhs = strings.TrimSpace(rhs)
	if !strings.HasPrefix(rhs, "context") {
		return model.TypeDecl{}, fmt.Errorf("ligne %d: seuls les types `context { ... }` sont supportés en v2", no)
	}
	open := strings.IndexByte(rhs, '{')
	closeB := strings.LastIndexByte(rhs, '}')
	if open < 0 || closeB < 0 || closeB < open {
		return model.TypeDecl{}, fmt.Errorf("ligne %d: type context : `{ ... }` attendu sur une ligne", no)
	}
	td := model.TypeDecl{Name: strings.TrimSpace(name), Line: no}
	for _, f := range strings.Split(rhs[open+1:closeB], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		fn, ft, ok := splitColon(f)
		if !ok {
			return model.TypeDecl{}, fmt.Errorf("ligne %d: champ de context: `nom: type` attendu, obtenu %q", no, f)
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
func parseCell(src string, no int, isOutput bool) (model.Cell, error) {
	s := strings.TrimSpace(src)
	cell := model.Cell{Src: s, Line: no}
	if s == "-" && !isOutput {
		cell.Dash = true
		return cell, nil
	}
	if s == "" {
		return model.Cell{}, fmt.Errorf("ligne %d: cellule vide", no)
	}
	node, err := feel.ParseString(s)
	if err != nil {
		return model.Cell{}, fmt.Errorf("ligne %d: cellule FEEL invalide %q: %w", no, s, err)
	}
	cell.Node = node
	return cell, nil
}

// --- helpers de découpe ---

func splitCells(s string) []string {
	parts := strings.Split(s, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
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
		return "", fmt.Errorf("ligne %d: type non supporté en v1: %q", no, s)
	}
}

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
		case strings.HasPrefix(t, "decision "):
			dec, err := p.parseDecision(t, ln.no)
			if err != nil {
				return nil, err
			}
			m.Decisions = append(m.Decisions, dec)
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
	name, typ, ok := splitColon(rest)
	if !ok {
		return model.Input{}, fmt.Errorf("ligne %d: `input` attend `nom : type`", no)
	}
	mt, err := parseType(typ, no)
	if err != nil {
		return model.Input{}, err
	}
	return model.Input{Name: name, Type: mt, Line: no}, nil
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
	mt, err := parseType(strings.TrimSpace(typ), no)
	if err != nil {
		return dec, err
	}
	dec.Type = mt
	p.i++ // consommer l'en-tête (le `{` en fin de ligne a déjà été validé plus haut)

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
	for _, c := range splitCells(lhs) {
		cell, err := parseCell(c, no, false)
		if err != nil {
			return model.Rule{}, err
		}
		r.Conds = append(r.Conds, cell)
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

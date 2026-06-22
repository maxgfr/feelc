// Package diag fournit un type d'erreur de compilation STRUCTURÉ et sérialisable
// ({file,line,col,code,message,suggestion}), tout en restant rétro-compatible avec
// le format texte historique ("ligne N: <message>") attendu par les tests existants.
//
// Discipline projet (jamais conformer en silence) : chaque site d'erreur du pipeline
// parse->compile produit un diag.Error explicite, avec un Code stable (catalogue ci-dessous)
// et, quand c'est utile, une Suggestion exploitable par la boucle red->green de la skill.
// La Suggestion N'EST PAS rendue par Error() (pour ne pas casser les assertions sur
// sous-chaînes) — uniquement dans le JSON et un rendu humain dédié.
package diag

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Catalogue de codes STABLES (consommés par la skill et docs/error-schema.md).
// Préfixe DSL = parseur ; CMP = compilateur. Ne pas renuméroter les codes existants.
const (
	// DSL — parseur .rules
	CodeUnknownStmt   = "DSL001" // instruction non reconnue
	CodeFeelSyntax    = "DSL002" // cellule/expression FEEL invalide
	CodeNoModel       = "DSL003" // modèle sans déclaration `model "..."`
	CodeModelHeader   = "DSL004" // en-tête `model` malformé
	CodeInputSyntax   = "DSL005" // `input` malformé
	CodeDecisionHead  = "DSL006" // en-tête de décision malformé
	CodeDecisionBody  = "DSL007" // ligne de corps de décision non reconnue
	CodeRuleSyntax    = "DSL008" // règle malformée
	CodeEmptyCell     = "DSL009" // cellule vide
	CodeTypeDecl      = "DSL010" // déclaration `type` malformée
	CodeUnknownType   = "DSL011" // type non supporté
	CodeBraceTrailing = "DSL012" // contenu après `{` sur la ligne d'en-tête

	// CMP — compilateur / typecheck
	CodeUndeclared   = "CMP001" // référence à un nom non déclaré
	CodeHitPolicy    = "CMP002" // hit policy non supportée
	CodeUnknownType2 = "CMP003" // type de décision inconnu
	CodeArity        = "CMP004" // mauvais nombre de conditions/sorties
	CodePriority     = "CMP005" // contrainte PRIORITY non satisfaite
	CodeCollect      = "CMP006" // contrainte COLLECT non satisfaite
	CodeUnsupported  = "CMP007" // construct hors sous-ensemble v2
	CodeLiteral      = "CMP008" // littéral attendu
)

// Error est un diagnostic de compilation structuré, positionné dans la source.
type Error struct {
	File       string // chemin source ("" -> préfixe texte historique "ligne N:")
	Line       int    // ligne 1-based (0 = inconnue -> pas de préfixe de position)
	Col        int    // colonne 1-based dans la ligne source (0 = inconnue)
	Code       string // code catalogue stable (ex: "DSL001")
	Message    string // message humain (FR) ; identique au texte historique
	Suggestion string // piste de correction (jamais dans Error(), seulement JSON/rendu humain)
	Cause      error  // erreur sous-jacente enveloppée (équivalent du %w historique)
}

// New crée un diagnostic positionné. line=0 si la position est inconnue.
func New(code string, line int, message string) *Error {
	return &Error{Code: code, Line: line, Message: message}
}

// Newf est la variante format de New.
func Newf(code string, line int, format string, args ...any) *Error {
	return &Error{Code: code, Line: line, Message: fmt.Sprintf(format, args...)}
}

// Wrap enveloppe une cause (équivalent de fmt.Errorf("...: %w")).
func Wrap(code string, line int, message string, cause error) *Error {
	return &Error{Code: code, Line: line, Message: message, Cause: cause}
}

// WithFile renseigne le fichier source (chaînable).
func (e *Error) WithFile(file string) *Error { e.File = file; return e }

// WithCol renseigne la colonne 1-based (chaînable).
func (e *Error) WithCol(col int) *Error { e.Col = col; return e }

// WithSuggestion attache une piste de correction (chaînable).
func (e *Error) WithSuggestion(s string) *Error { e.Suggestion = s; return e }

// WithCause attache une cause enveloppée (chaînable).
func (e *Error) WithCause(c error) *Error { e.Cause = c; return e }

// Error rend le diagnostic au format texte. Compatible historique :
//   - File=="" et Line>0  -> "ligne N: <message>"
//   - File!=""            -> "file:line[:col]: <message>"
//   - Line==0 et File=="" -> "<message>" (erreur globale sans position)
//
// La cause éventuelle est rendue après ": " (comme le %w historique). La suggestion
// n'apparaît JAMAIS ici.
func (e *Error) Error() string {
	prefix := ""
	switch {
	case e.File != "" && e.Line > 0:
		if e.Col > 0 {
			prefix = fmt.Sprintf("%s:%d:%d: ", e.File, e.Line, e.Col)
		} else {
			prefix = fmt.Sprintf("%s:%d: ", e.File, e.Line)
		}
	case e.Line > 0:
		prefix = fmt.Sprintf("ligne %d: ", e.Line)
	}
	msg := e.Message
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return prefix + msg
}

// Unwrap expose la cause pour errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// MarshalJSON sérialise {file,line,col,code,message,suggestion} (omitempty sur
// file/col/code/suggestion ; line et message toujours présents).
func (e *Error) MarshalJSON() ([]byte, error) {
	type jsonErr struct {
		File       string `json:"file,omitempty"`
		Line       int    `json:"line"`
		Col        int    `json:"col,omitempty"`
		Code       string `json:"code,omitempty"`
		Message    string `json:"message"`
		Suggestion string `json:"suggestion,omitempty"`
	}
	return json.Marshal(jsonErr{
		File: e.File, Line: e.Line, Col: e.Col,
		Code: e.Code, Message: e.Message, Suggestion: e.Suggestion,
	})
}

// WithFileIfDiag stampille le fichier sur le *diag.Error éventuel d'une chaîne
// d'erreurs (no-op si err n'enveloppe pas de *diag.Error ou si File déjà renseigné).
// Point unique de propagation du nom de fichier en remontée du pipeline.
func WithFileIfDiag(err error, file string) error {
	if err == nil || file == "" {
		return err
	}
	var de *Error
	if errors.As(err, &de) && de.File == "" && de.Line > 0 {
		de.File = file
	}
	return err
}

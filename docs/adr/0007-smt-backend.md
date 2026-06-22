# ADR 0007 — Backend SMT (Z3) optionnel, derrière un build tag

- **Statut** : accepté (2026-06-22)
- **Décideurs** : maxgfr

## Contexte

La vérification de feelc repose sur une **algèbre d'hyper-rectangles** (décomposition géométrique
en cellules atomiques). Elle couvre les tables dont toutes les cellules sont des `CellTest`
normalisés. Les cellules `Op=Prog` (expression libre : arithmétique inter-colonnes, référence à
une autre colonne) ne sont **pas** décidables géométriquement — elles sont signalées en
**dégradation honnête** `not-verifiable` (ADR 0004 §2). Un solveur SMT (Z3) peut décider la
complétude/les conflits sur ces résidus.

## Décision

Backend SMT **optionnel**, **isolé derrière le build tag `smt`** — hors du chemin critique, zéro
dépendance (ni CGo ni binaire) dans le build par défaut.

- **Point d'extension** : `var smtProve func(cm, d, rep) bool` dans `internal/verify/verify.go`.
  nil par défaut → comportement inchangé (`not-verifiable` sur `Op=Prog`). Un backend renvoie
  `true` s'il a traité la décision.
- **Encodeur PUR et testable** : `internal/smt` traduit la couche géométrique (`CellTest`) et le
  bytecode straight-line (`ExprProgram`) en SMT-LIB2 (théorie Reals + Bools). **Sans dépendance
  externe → unitairement testé sans Z3.** Sous-ensemble : arithmétique, comparaisons, and/or/not,
  intervalles, ensembles, négation, colonnes number/boolean. Hors sous-ensemble (if/then/else
  compilé en sauts, floor/ceiling/round, colonnes string, dépendances décision) → refusé
  proprement (`ok=false`).
- **Câblage Z3** : `internal/verify/verify_smt.go` (`//go:build smt`) branche `smtProve`, encode
  une requête de complétude (`unsat` ⇒ table complète), et invoque `z3 -in`.

**Dégradation HONNÊTE (jamais conformer en silence)** : z3 absent du PATH, ou forme hors
sous-ensemble encodable → `not-verifiable` avec la raison ; jamais de fausse preuve.

## Conséquences

- Build par défaut : aucun impact (point d'extension nil), reproductible et sans dépendance.
- Build `-tags smt` : `go build -tags smt`, requiert `z3` dans le PATH pour une preuve effective
  (sinon `not-verifiable` explicite). Reflète les opcodes finaux (post-Tranche 22 : if/built-ins).
- L'encodeur est testé ; le chemin de preuve Z3 effectif est validé là où `z3` est installé.
- **Reprise** : étendre l'encodeur (if/then/else via `ite`, `floor/ceiling/round` via `to_int`),
  et router aussi la détection de conflits vers SMT.

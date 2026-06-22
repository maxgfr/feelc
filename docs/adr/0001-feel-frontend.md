# ADR 0001 — Front-end FEEL : dépendance de parsing vs parser maison

- **Statut** : accepté (Tranche 0, 2026-06-22)
- **Décideurs** : maxgfr
- **Contexte technique** : feelc a besoin de parser deux choses — les **cellules de table** (unary
  tests : `< 580`, `[580..680)`, `"a","b"`, `not(...)`, `-`) et les **expressions** des décisions
  literal-expression (`monthly_debt / (annual_income / 12)`).

## Contexte

Le plan envisageait un **parser FEEL maison** (Pratt + double racine, ~1500-2500 lignes) comme chemin
par défaut probable, la revue adverse estimant le candidat `pbinitiative/feel` insuffisant (basée sur une
version périmée : « 7 stars, pas d'unary tests »). La règle du gate : **mesurer avant de planifier**.

## Évaluation empirique (spike `spike/`)

`pbinitiative/feel@v1.0.6` (MIT) a été évalué contre 20 unary-tests + 10 expressions représentatifs
des 4 exemples (voir `spike/main.go`). Résultat : **17/20 unary-tests, 10/10 expressions**.

Constats clés :
- `Parse()` / `ParseString` parsent **en contexte unary-test par défaut** : `< 580` donne
  `Binop{Left: Var{"?"}, Op:"<", Right: 580}` — exactement la sémantique « comparaison implicite sur la
  valeur de colonne » dont on a besoin. Une expression normale retombe sur `p.expression()`.
- **AST exporté et exploitable** : `Binop`, `RangeNode{StartOpen,Start,EndOpen,End}`, `MultiTests`,
  `NumberNode{Value string}` (**littéral source préservé** → re-parsable exactement avec apd),
  `StringNode`, `Var`, `FunCall`, `IfExpr`, `BoolNode`, tous porteurs d'un `TextRange` (positions source).
- Les 3 échecs sont **non bloquants** : `-` (any/don't-care) est un marqueur **niveau DSL**, traité
  avant le parseur ; `]0..100]` est une notation alternative qu'on n'autorise pas (la forme standard
  `(0..100]`/`[0..100)` marche) ; `not(< 18)` (unary-test négué avec opérateur) est un manque étroit à
  traiter dans notre normaliseur de cellules ou à différer.
- **Limite assumée** : leur type `Number` est un `big.Float` (binaire, 272 bits), **non décimal exact**,
  et leur interprète est un tree-walker. Ni l'un ni l'autre ne convient au déterminisme de feelc.

## Décision

1. **Utiliser `github.com/pbinitiative/feel` comme dépendance** pour le **lexer + parser + AST** des
   expressions et unary-tests. Pas de fork initial : l'API publique (`ParseString`) suffit et la licence
   est MIT.
2. **Écrire nous-mêmes** le typecheck, le lowering vers l'IR, et la VM. **Ne PAS utiliser** leur
   interprète (`EvalString`) ni leur `Number` (`big.Float`).
3. Le décimal exact passe par **apd** (cf. [ADR 0002](./0002-decimal.md)) en re-parsant
   `NumberNode.Value` (le littéral source).
4. Gérer `-` (any) et, si besoin, `not(<test>)` dans **notre couche DSL/normalisation de cellules**.
5. **Réévaluer le fork/vendoring** seulement si l'on doit corriger les 2 gaps (`not(<test>)`, notations
   de range) ou si une dérive amont apparaît. Épingler la version exacte dans `go.mod`.

## Conséquences

- **Gain majeur** : ~2000 lignes de parser maison évitées ⇒ Tranches 1-2 fortement dé-risquées.
- **Dépendance externe** dans le cœur (vs « zéro-dép » idéal) — acceptable : MIT, petite, AST stable.
  Mitigation : le typecheck est notre **gardien de périmètre** (rejette tout construct hors sous-ensemble),
  et un test de couverture du sous-ensemble protège contre les régressions amont.
- **Déterminisme préservé** : on n'hérite d'aucun float ; le parsing est pur (le littéral reste une string).
- Spike conservé sous `spike/` (module séparé, exclu du build principal) comme **preuve reproductible**.

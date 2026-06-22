# ADR 0004 — Reports assumés : BKM paramétré et extension SMT

- **Statut** : accepté (2026-06-22)
- **Décideurs** : maxgfr

Conformément à l'éthique du projet (« jamais conformer/prétendre en silence »), on documente
explicitement deux fonctionnalités du périmètre « complet » qui sont **reportées**, avec leur raison.

## 1. BKM / Invocation paramétrée (Tranche 7) — ✅ LEVÉ (2026-06-22, Tranche 13)

> **Statut mis à jour : report LEVÉ.** Le BKM paramétré est implémenté. On a **forké** et vendorisé
> `pbinitiative/feel` sous `third_party/feel` (épinglé via `replace`) pour **exporter `FunCall.Args`**
> (`[]FunCallArg{Name, Arg}`), ce qui débloque la lecture des arguments. La syntaxe est
> `bkm name(p:t, …):ret = expr` (signature parsée côté DSL, pas dans le fork) ; l'invocation
> `name(a, b)` est **inlinée à la compilation** par substitution AST des paramètres
> (`internal/compiler/lower_expr.go`) — **zéro nouvel opcode**, la VM/IR inchangées. Récursion
> (auto/mutuelle) détectée statiquement et **rejetée franchement** ; garde-fous de profondeur et de
> budget d'instructions (RAM bornée). Le fork corrige aussi un **DoS amont** (boucle infinie du
> parseur sur un `?` explicite). Le constat historique ci-dessous est conservé pour mémoire.

**Constat technique (historique).** feelc réutilise `github.com/pbinitiative/feel` comme parseur FEEL
(ADR 0001). Or son nœud d'AST `FunCall` expose `Args []funcallArg` où **`funcallArg` est un type
non exporté** dont les champs (`argName`, `arg`) sont eux aussi non exportés. Il est donc impossible,
via l'API publique, de **lire les arguments d'un appel de fonction** `nom(arg1, arg2)`. Implémenter
une invocation BKM paramétrée nécessiterait de **forker** le parseur (ce que l'ADR 0001 anticipait
comme recours).

**Décision.** Reporter le BKM paramétré. Coût/valeur défavorable maintenant :
- **Aucun des 4 exemples de référence** n'en a besoin (la revue adverse l'avait souligné).
- La **réutilisation non-paramétrée** est déjà couverte : une décision literal-expression
  (`decision x : number = …`) est une expression nommée réutilisable, référencée par son nom dans
  les `needs:` d'autres décisions (DRG).

**Reprise.** Forker `pbinitiative/feel` pour exporter `funcallArg` (ou écrire le parseur FEEL maison
prévu en repli par l'ADR 0001), puis inliner les invocations à la compilation (pas de frame d'appel).

## 2. Extension SMT (Z3) — reporté (optionnel, derrière build tag)

La vérification formelle (Tranche 4) repose sur une **algèbre d'hyper-rectangles** : elle couvre les
tables dont toutes les cellules sont des `CellTest` normalisés (comparaisons, intervalles, ensembles).
Les cellules `Op=Prog` (référence à une autre colonne, arithmétique inter-colonnes) ne sont **pas**
décidables géométriquement et sont déjà signalées en **dégradation honnête** (`not-verifiable`).

**Décision.** L'extension SMT (Z3) pour prouver des propriétés sur ces cellules non-rectangulaires
reste **optionnelle et différée**, derrière un build tag, hors du chemin critique. La géométrie
couvre l'essentiel des tables DMN sans dépendance externe (ni CGo ni binaire Z3).

**Reprise.** Quand un besoin réel de conditions inter-colonnes prouvables apparaît : intégrer Z3
(via binaire ou binding) sous `//go:build smt`, et router les cellules `Op=Prog` vers le solveur.

# ADR 0003 — Sémantique de null et des erreurs (trivalence)

- **Statut** : accepté (Tranche 2, 2026-06-22)
- **Décideurs** : maxgfr

## Contexte

FEEL est **trivalent** : `null` est une valeur de première classe et se propage sans lever
d'exception. C'est précisément ce qui fait échouer ~30 % du DMN TCK aux implémentations naïves
(la revue adverse l'a signalé). Il faut donc **figer une table de décision null/erreur explicite**
et la tester, *avant* de viser le TCK. feelc distingue trois mondes : la frontière d'entrée, les
cellules de table, et les expressions.

## Décision (politique v2)

### 1. Frontière d'entrée
- Une entrée externe **manquante** référencée par une décision → **erreur** (`variable inconnue à
  l'exécution`). C'est une violation de contrat de l'appelant, pas un `null` FEEL. Fail-fast.
- Une entrée explicitement `null` (JSON `null`) → valeur `null` FEEL qui suit les règles ci-dessous.

### 2. Cellules de table (unary tests)
- Une cellule testée contre une valeur `null` (`< 580`, `[a..b)`, `= x`, ensemble) → **ne matche pas**
  (`false`), **sans erreur**. `null` ne satisfait aucune condition. → la ligne `default` (si présente)
  prend le relais ; sinon la décision vaut `null`.
- Un **type incohérent non-null** dans une cellule (ex. comparer un `string` à un seuil numérique alors
  que le typecheck aurait dû l'interdire) → **erreur** (anomalie réelle, pas un cas métier).

### 3. Décisions
- Table sans règle gagnante **et sans `default`** → résultat **`null`** (et la **vérification de
  complétude (Tranche 4) signalera le trou** avec un contre-exemple — on ne masque rien en silence).
- Expression : **arithmétique avec un opérande `null`** → propage **`null`** (jamais d'exception).
- **Division par zéro** → **erreur** (cas indéfini, distinct de la propagation de null ; choix orienté
  auditabilité : une division par zéro est un défaut du modèle/des données, pas un résultat métier).

## Déféré (assumé, jamais conformé en silence)

- **Logique booléenne trivalente complète** au niveau expression (`null and false`, `null or true`,
  comparaison `null < x` → `null` plutôt que `false`). En v2 une comparaison sur `null` dans une
  **expression** rend `false` (conservateur). La trivalence booléenne fine arrivera avec le harness
  DMN TCK (cf. plan), accompagnée de tests dédiés. Aucune prétention de conformité TCK tant que ce
  n'est pas implémenté.

## Conséquences

- Comportement **déterministe et testé** sur les cas null courants des 4 exemples.
- Les cellules tolèrent `null` (robustesse : une décision amont qui rend `null` n'explose pas l'aval,
  elle tombe sur le `default`).
- La frontière (input manquant, division par zéro) **échoue franchement** plutôt que de produire un
  résultat trompeur — cohérent avec l'objectif d'auditabilité.

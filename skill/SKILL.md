---
name: feelc-rules
description: >-
  Author, verify and test business rules in the feelc DSL (a DMN/FEEL decision language compiled
  to a deterministic Go engine). Use when the user wants to WRITE or REVIEW business rules /
  decision logic and have them be deterministic, auditable and formally checkable — e.g.
  "encode these eligibility rules", "write a decision table", "rule engine", "DMN", "FEEL",
  "barème / éligibilité / scoring / tarification / promotions en règles", "génère les règles
  métier", "vérifie ma table de décision", "feelc". The AI writes the .rules source; the feelc
  binary (compile / verify / run) is the deterministic oracle — never decide rule outcomes "in
  your head", always run feelc.
license: Apache-2.0
metadata:
  version: 0.2.0
---

# feelc-rules — écrire des règles métier vérifiables

Cette skill aide à **rédiger, vérifier et tester** des règles métier dans le langage **feelc**
(paradigme DMN : un graphe de décisions liées, chacune une table de décision ou une expression,
le tout en FEEL). feelc compile la source en moteur **déterministe** : à l'exécution, aucun LLM,
décisions reproductibles et auditables. **Ton rôle = rédiger le DSL** ; le binaire `feelc` est
l'**oracle déterministe** (compilation + vérification + évaluation). Ne décide JAMAIS d'un résultat
de règle « de tête » : lance toujours `feelc`.

## Quand l'utiliser

- L'utilisateur veut encoder une politique métier (éligibilité, scoring, tarification, barème,
  remises, droits/prestations…) en règles exécutables.
- Il veut une table de décision **vérifiée** (pas de trou, pas de conflit) et **rejouable**.
- Il mentionne DMN, FEEL, « moteur de règles », ou `feelc`.

## Pré-requis : le binaire `feelc`

Toutes les commandes passent par le wrapper portable `scripts/feelc-skill.mjs`, qui localise
`feelc` (variable `FEELC_BIN`, PATH, checkout voisin, ou build via `go build`). Vérifie d'abord :

```sh
node scripts/feelc-skill.mjs version
```

Si ça échoue, le wrapper imprime comment fournir `feelc` (voir `references/install.md`).
Tu peux aussi appeler `feelc` directement s'il est sur le PATH.

## Le flux (red → green, piloté par l'oracle)

Crée une todo par étape et suis-les dans l'ordre.

1. **Interviewer** (ne devine pas). Établis : les **entrées** (Input Data) et leurs **domaines**
   (`in [a..b]`, `>= 0`, `in {…}`), les **décisions** et leurs dépendances, la **hit policy** de
   chaque table, les **cas limites**. Voir `references/authoring.md`.
2. **Rédiger** le fichier `.rules`. Reste STRICTEMENT dans le sous-ensemble supporté
   (`references/feel.md`) — tout construct hors-périmètre fait échouer la compilation, c'est
   voulu. Utilise les 4 modèles de `references/examples.md` comme gabarits.
3. **Compiler + vérifier** (gate déterministe) :
   ```sh
   node scripts/feelc-skill.mjs verify --rules model.rules --json
   ```
   - Erreurs de **compilation** (type, référence, syntaxe) → avec `--json`, elles sortent en objet
     structuré `{file,line,col,code,message,suggestion}` sur stdout : exploite `line`/`col` pour
     localiser et `suggestion` pour corriger, puis relance. Catalogue de codes stables :
     `docs/error-schema.md`.
   - **Bloqueurs** de vérification (`severity: "error"` : trou de complétude avec contre-exemple,
     conflit UNIQUE/ANY) → corrige. Lis `references/verify.md`.
4. **Tester** sur des cas concrets (y compris les cas limites de l'interview) :
   ```sh
   node scripts/feelc-skill.mjs run --rules model.rules --decision <nom> --input '{…}' --json
   ```
   Compare à l'attendu. Itère jusqu'à concordance.
5. **(Optionnel) Gate Layer-2 sémantique** — vérifier que les règles disent bien ce que le besoin
   voulait. Décompose la spec/le besoin en **claims** atomiques `{decision, input, expect}` (TON
   travail d'IA), écris-les dans un `claims.json` (`{"claims":[…]}`), puis laisse la VM trancher :
   ```sh
   node scripts/feelc-skill.mjs check --rules model.rules --claims claims.json --json
   ```
   `supported` = la règle confirme le claim ; `contradicted`/`error` = bloquant (la règle ne fait
   pas ce que le besoin disait, ou ton claim est faux → corrige l'un ou l'autre). « Le LLM propose,
   la VM dispose » : n'invente jamais un seuil pour faire passer un claim.
6. **Itérer** jusqu'au critère d'arrêt.

## Critère d'arrêt : « zéro bloqueur, pas zéro finding »

- **Buildable** = `verify` ne renvoie **aucun bloqueur** (`severity: "error"`). Les `warning`
  (règle masquée…) et `info` (default inutile…) sont à **signaler**, pas forcément à corriger.
- **Convergent** = `run` reproduit les cas de référence/limites validés avec l'utilisateur.

N'invente jamais un seuil ou une valeur pour « faire passer » : si une ambiguïté du besoin
est irréductible, **remonte la question à l'utilisateur**.

## À NE PAS faire

- ❌ Ne calcule pas un résultat de règle toi-même — lance `feelc run`.
- ❌ N'utilise pas de constructs hors sous-ensemble (fonctions FEEL, `if/then/else` dans une
  expression, regex, dates/fuseaux, lambdas) : ça ne compile pas. Voir `references/feel.md`.
- ❌ Ne laisse pas une table single-hit (FIRST/UNIQUE/ANY/PRIORITY) **incomplète** sans `default`
  assumé : `verify` signalera un trou avec un contre-exemple.
- ❌ Ne maquille pas un `warning`/`info` en succès, et ne supprime pas une règle juste pour
  taire un diagnostic sans comprendre pourquoi.

## Références (divulgation progressive)

- `references/authoring.md` — l'interview et la structure d'un modèle.
- `references/feel.md` — le sous-ensemble DSL/FEEL supporté (et ce qui ne l'est pas).
- `references/verify.md` — lire `feelc verify`, bloqueurs vs remarques.
- `references/examples.md` — les 4 modèles de référence comme gabarits.
- `references/forbidden-patterns.md` — pièges classiques (chevauchement, défaut manquant…).
- `references/install.md` — fournir le binaire `feelc`.

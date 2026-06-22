# Schéma d'erreur structurée feelc

Les erreurs de **compilation** (parse `.rules` + typecheck/lowering) sont des objets
structurés et positionnés, sérialisables en JSON via `--json`. C'est le contrat que la
skill d'autoring lit pour sa boucle red→green (corriger la source à partir de
`line`/`col`/`suggestion`).

## Format texte (toujours)

`Error()` reste **rétro-compatible** :

- sans fichier connu : `ligne <N>: <message>`
- avec fichier        : `<file>:<line>[:<col>]: <message>`
- erreur globale (sans position) : `<message>`

La `suggestion` n'apparaît **jamais** dans le texte (uniquement en JSON / rendu humain
dédié) afin de ne pas casser les assertions sur sous-chaînes.

## Format JSON (`--json`)

Émis sur **stdout** par `feelc run|verify|check --json` quand la compilation échoue :

```json
{
  "file": "credit.rules",
  "line": 12,
  "col": 7,
  "code": "DSL002",
  "message": "cellule FEEL invalide \"1 +\": ...",
  "suggestion": "..."
}
```

| Champ        | Type   | Présence                                  |
|--------------|--------|-------------------------------------------|
| `file`       | string | omis si inconnu                           |
| `line`       | int    | toujours (0 si position inconnue)         |
| `col`        | int    | omis si 0 (inconnue) ; 1-based            |
| `code`       | string | omis si vide ; **stable** (voir ci-dessous) |
| `message`    | string | toujours ; texte FR identique au texte    |
| `suggestion` | string | omis si vide                              |

`col` est calculée **au split DSL** (offset du segment de cellule dans la ligne source).
Elle est fiable pour les cellules de table (conditions/sorties) ; pour les expressions
literal-expression et déclarations sur une ligne, seule `line` est garantie.

## Catalogue de codes (STABLE — ne pas renuméroter)

Consommé par la skill : ces codes sont un contrat. Sources : `internal/diag/diag.go`.

### `DSL*` — parseur `.rules`

| Code   | Sens                                              |
|--------|---------------------------------------------------|
| DSL001 | instruction non reconnue                          |
| DSL002 | cellule / expression FEEL invalide (enveloppe la cause FEEL) |
| DSL003 | modèle sans déclaration `model "..."`             |
| DSL004 | en-tête `model` malformé                          |
| DSL005 | `input` malformé                                  |
| DSL006 | en-tête de décision malformé                      |
| DSL007 | ligne de corps de décision non reconnue           |
| DSL008 | règle malformée (`=>` manquant)                   |
| DSL009 | cellule vide                                      |
| DSL010 | déclaration `type` malformée                      |
| DSL011 | type non supporté                                 |
| DSL012 | contenu après `{` sur la ligne d'en-tête          |

### `CMP*` — compilateur / typecheck

| Code   | Sens                                              |
|--------|---------------------------------------------------|
| CMP001 | référence à un nom non déclaré (`needs`/var)      |
| CMP002 | hit policy non supportée                          |
| CMP003 | type de décision inconnu                          |
| CMP004 | mauvais nombre de conditions / sorties            |
| CMP005 | contrainte PRIORITY non satisfaite                |
| CMP006 | contrainte COLLECT non satisfaite                 |
| CMP007 | construct hors sous-ensemble v2                   |
| CMP008 | littéral attendu                                  |

## Portée

Ce schéma couvre **les erreurs de compilation**. Les rapports de `verify` (`Finding`) et
`check` (`Verdict`) ont leur propre forme JSON (alignée sur le même style), inchangée ici.
La cause FEEL brute reste enveloppée (`Unwrap()` / `errors.As`), jamais réécrite.

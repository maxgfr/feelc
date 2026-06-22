# Sous-ensemble DSL / FEEL supporté par feelc (v2)

> Le typecheck est le gardien : tout construct hors de ce périmètre **fait échouer la
> compilation** avec un message clair. C'est voulu (refuser plutôt qu'accepter-puis-mal-interpréter).

## Structure d'un fichier `.rules`

```
model "nom" {
  rounding: half_even          # optionnel
}

input <nom> : <type> [<domaine>]
type <Nom> = context { f1: t1, f2: t2 }     # type de sortie multi-champs (optionnel)

# Décision = expression littérale (type scalaire) :
decision <nom> : <type> = <expression FEEL>

# Décision = table :
decision <nom> : <type|TypeContext> {
  needs: a, b, c                 # colonnes d'entrée (inputs OU décisions amont -> DRG)
  hit: <politique>
  priority: v1, v2, ...          # UNIQUEMENT si hit: priority (du + au - prioritaire)
  cellule | cellule | ... => sortie | sortie | ...
  default  | ...        => sortie | ...        # optionnel : sinon non-match = null
}
```

## Types

`number` (décimal **exact**, jamais flottant), `string`, `boolean`, et les types `context { … }`
déclarés (sortie multi-colonnes). Une décision `: number|string|boolean` a **une** sortie scalaire ;
une décision `: MonContext` a une sortie par champ du context (autant de colonnes de sortie).

## Domaines d'entrée (servent à la vérification de complétude)

`in [a..b]` · `[a..b)` · `(a..b]` · `(a..b)` · `>= x` · `> x` · `<= x` · `< x` · `in {v1, v2, …}`

## Cellules de condition (unary tests)

| Forme | Sens |
|---|---|
| `-` | n'importe quoi (don't care) |
| `580`, `"gold"`, `true` | égalité au littéral |
| `< x` `<= x` `> x` `>= x` `!= x` | comparaison (x = littéral, **ou une autre colonne/variable**) |
| `[a..b)` etc. | intervalle (bornes ouvertes/fermées) |
| `"a","b"` ou `1,2,3` | ensemble = OU (appartenance) |
| `not(littéral)` | négation d'un littéral |

⚠️ `not(< 18)` (négation d'une comparaison) **n'est pas** supporté.

## Sorties de table

**Littéraux uniquement** (`true`, `"texte"`, `42`). Une sortie calculée se fait via une **décision
literal-expression** séparée (`decision x : number = …`), pas dans une cellule de sortie.

## Expressions (dans `decision … = <expr>` et cellules `?`-vs-colonne)

Supporté : littéraux, variables (input ou décision amont), `+ - * /`, comparaisons
`< <= > >= = !=`, `and`, `or`, parenthèses. Exemple : `monthly_debt / (annual_income / 12)`.

**NON supporté en v2** (échoue à la compilation) : appels de fonction (`sum(...)`, `floor(...)`…),
`if/then/else`, `not(...)` en expression, `**`, listes/ranges en expression, moins unaire,
dates/durées/fuseaux. ⚠️ `sum`/`min`/`max`/`count` n'existent **pas** comme fonctions FEEL — ce
sont des **agrégations de hit policy COLLECT** (voir ci-dessous).

## Hit policies

| `hit:` | Sémantique |
|---|---|
| `unique` | au plus 1 règle matche ; ≥2 → **erreur** |
| `any` | plusieurs peuvent matcher mais **mêmes sorties** ; sorties divergentes → **erreur** |
| `first` | la 1re règle qui matche gagne (l'ordre = la priorité) |
| `priority` | parmi les règles qui matchent, la sortie la plus prioritaire (ligne `priority:`) |
| `collect` | liste de toutes les sorties qui matchent |
| `collect sum` / `min` / `max` / `count` | agrégation numérique des sorties qui matchent |
| `rule order` | liste des sorties, dans l'ordre des règles |

## Valeurs `null` et erreurs (déterministe, figé)

- Cellule testée contre `null` → **ne matche pas** (tombe sur `default`, sinon décision = `null`).
- Arithmétique avec un opérande `null` → résultat **`null`** (propagation).
- **Division par zéro** → **erreur**.
- Entrée requise **manquante** → **erreur**.

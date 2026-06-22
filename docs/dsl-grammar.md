# Grammaire du DSL `.rules`

Le langage source de feelc (la **source de vérité**) est volontairement minimal et **orienté
lignes**. Parseur : `internal/dsl`. Tout construct hors sous-ensemble **échoue franchement**
(refuser plutôt qu'accepter-puis-mal-interpréter).

## Structure d'un fichier

```
model "<nom>" {}

input <nom> : <type> [<domaine>]
...

type <Nom> = context { <champ>: <type>, ... }      # optionnel
bkm <nom>(<p>:<type>, ...): <type> = <expr FEEL>    # optionnel

decision <nom> : <type> = <expr FEEL>               # décision literal-expression
decision <nom> : <type> {                            # décision table
  needs: <a>, <b>, ...
  hit: <politique>
  priority: <v1>, <v2>, ...                          # uniquement si hit: priority
  <cond> | <cond> => <sortie> | <sortie>
  default        => <sortie> | <sortie>              # optionnel
}
```

- `# ...` : commentaire (hors chaîne), retiré à la lecture (**non préservé** par `feelc fmt`).
- `<type>` : `number`, `string`, `boolean`, ou un nom de `type ... = context {...}` déclaré.

## Déclarations

| Forme | Sens |
|-------|------|
| `model "credit" {}` | nom du modèle (le corps `{ rounding: ... }` est ignoré, non stocké) |
| `input credit_score : number in [300..850]` | donnée d'entrée + **domaine** (vérif de complétude) |
| `type Out = context { ok: boolean, label: string }` | type de sortie multi-colonnes |
| `bkm dti(d:number, i:number):number = d / (i / 12)` | fonction pure paramétrée, **inlinée** à la compilation |

### Domaines d'entrée (optionnels)

`in [a..b]` / `in (a..b)` (bornes ouvertes), `>= 0`, `> 0`, `<= 100`, `< 100`, `in { "a", "b" }`
(énumération). Une forme non reconnue est ignorée (pas de domaine).

## Décisions

- **literal-expression** : `decision x : number = <expr>` — une expression FEEL (cf.
  [feel-subset.md](feel-subset.md)). `?` (valeur de colonne) y est **interdit** (réservé aux cellules).
- **table** : `needs:` (colonnes d'entrée), `hit:` (politique), règles, `default` optionnel.

### Hit policies

`first`, `unique`, `any`, `priority` (+ ligne `priority:`), `rule order`,
`collect` / `collect sum` / `collect min` / `collect max` / `collect count`.

### Cellules

Une cellule de **condition** est un *unary test* FEEL : `-` (any), littéral (`580`, `"urban"`,
`true`), comparaison (`< 580`, `>= 18`), intervalle (`[580..680)`), ensemble (`"a", "b"`),
négation (`not(<test>)`), ou expression libre référençant `?`/d'autres colonnes (compilée en
bytecode, dite *Op=Prog*, non géométrique). Une cellule de **sortie** est un littéral.

## Erreurs

Toute erreur de compilation est un diagnostic **structuré** positionné — voir
[error-schema.md](error-schema.md) (`--json` → `{file,line,col,code,message,suggestion}`).

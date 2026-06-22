# Rédiger un modèle feelc — l'interview

Ne devine pas la logique métier : élicite-la. Pose ces questions (regroupe-les, mais couvre tout) :

## 1. Les entrées (Input Data)
- Quelles données alimentent la décision ? Pour chacune : **nom**, **type** (number/string/boolean),
  et surtout son **domaine** (`number in [0..120]`, `>= 0`, `string in {"urban","rural"}`).
  → Les domaines rendent la **complétude vérifiable** : déclare-les dès que possible.

## 2. Les décisions et leur graphe (DRG)
- Quelle(s) **décision(s) finale(s)** ? Quelles **décisions intermédiaires** (ex. un ratio, un
  score) ? Une décision peut dépendre d'une autre via `needs:` → feelc l'évalue à la demande.
- Une décision intermédiaire calculée = **literal-expression** : `decision dti : number = a / b`.
- Une décision à base de cas = **table**.

## 3. Pour chaque table : la hit policy
- Les cas sont-ils **mutuellement exclusifs** ? → `unique` (et `verify` prouvera l'exclusivité).
- Veut-on l'**ordre de priorité** des lignes ? → `first` (refus prioritaires d'abord, p. ex.).
- Plusieurs effets **cumulables** ? → `collect` (liste) ou `collect sum` (somme).
- La **meilleure** valeur ? → `collect max` / `collect min`.
- Voir `references/feel.md` pour la liste complète.

## 4. La sortie
- Une seule valeur → type scalaire (`: number` / `: string` / `: boolean`).
- Plusieurs champs (ex. `{eligible, reason}`) → déclare un `type … = context { … }`.

## 5. Les cas limites
- Que se passe-t-il aux **bornes** (égalité, min/max du domaine) ? Encode-les explicitement.
- Y a-t-il un cas **par défaut** ? Pour une table single-hit, ajoute une ligne `default` si tous
  les cas ne sont pas couverts (sinon `verify` signalera un trou — ce qui est souvent le signal
  qu'il manque une règle).

## Squelette de départ

```
model "<domaine>" {}

input ... : ...
type Resultat = context { ... }      # si sortie multi-champs

decision <intermediaire> : number = <expr>     # si besoin

decision <final> : Resultat {
  needs: ...
  hit: first
  ... | ... => ... | ...
  default | => ...
}
```

Puis : `verify` (gate déterministe) → `run` sur les cas limites → itère.

# Fournir le binaire `feelc`

Le wrapper `scripts/feelc-skill.mjs` cherche `feelc` dans cet ordre :

1. **`$FEELC_BIN`** — chemin explicite vers un binaire feelc.
   ```sh
   export FEELC_BIN=/chemin/vers/feelc
   ```
2. **`feelc` sur le PATH** — si tu l'as installé globalement.
3. **Binaire à la racine du dépôt** — `../../feelc` (la skill vit dans `feelc/skill/`).
4. **Build automatique** — si la skill tourne dans le dépôt (`../../go.mod` présent) et que `go`
   est installé, le wrapper lance `go build -o ../../feelc ./cmd/feelc`.

> La skill est intégrée au dépôt `feelc` (sous `skill/`). En usage DANS le dépôt, 3/4 suffisent.
> En installation autonome (copie de la skill seule), utilise plutôt 1 (`$FEELC_BIN`) ou 2 (PATH).

## Obtenir feelc

- Cloner et builder (Go ≥ 1.22) :
  ```sh
  git clone https://github.com/maxgfr/feelc
  cd feelc && go build -o feelc ./cmd/feelc
  export FEELC_BIN="$PWD/feelc"
  ```
- Vérifier : `node skill/scripts/feelc-skill.mjs version` → `feelc <version>`.

Le wrapper relaie ensuite toutes les sous-commandes telles quelles : `verify`, `run`, `serve`, `version`.

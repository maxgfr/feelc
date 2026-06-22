# ADR 0005 — Erreurs de compilation structurées et positionnées

- **Statut** : accepté (2026-06-22)
- **Décideurs** : maxgfr

## Contexte

Les erreurs de compilation (`dsl.Parse` + `compiler.Compile`) étaient des `fmt.Errorf` plats
au format `"ligne %d: <message>"`. La skill d'autoring a besoin, pour sa boucle red→green, d'un
diagnostic **machine-exploitable** : fichier, ligne, **colonne**, code stable et suggestion de
correction. Par ailleurs `model.Cell.Col` était déclaré mais **jamais renseigné** (toujours 0).

## Décision

Introduire `internal/diag.Error{File, Line, Col, Code, Message, Suggestion, Cause}` qui :

1. **Reste rétro-compatible** en texte : `Error()` rend exactement `"ligne N: <message>"` quand
   aucun fichier n'est connu (les tests existants matchent des sous-chaînes FR — filet de sécurité
   anti-régression). Avec un fichier : `"file:line[:col]: message"`. La **suggestion n'est jamais**
   dans `Error()`.
2. Expose `MarshalJSON` → `{file,line,col,code,message,suggestion}` (omitempty), rendu sur stdout
   par `feelc run|verify|check --json`.
3. Préserve le chaînage `%w` historique via `Cause` + `Unwrap()` (les erreurs FEEL brutes restent
   enveloppées, jamais réécrites).

**Positions.** `model.Cell.Col` est désormais rempli, **calculé au split DSL** (offset cumulé du
segment de cellule dans la ligne source). Piège évité : `feel.Node.TextRange().Column` est relatif
à la cellule isolée (chaque cellule est parsée seule) — inexploitable comme colonne de ligne. La
propagation du **nom de fichier** passe par `dsl.ParseFile(path, src)` / `loader.CompileFile(path, src)` ;
`Parse`/`Compile` restent des wrappers `file=""` (compat). `engine.Run` reste `file=""`.

**Codes.** Catalogue stable `DSL*` / `CMP*` figé dans `docs/error-schema.md` — contrat consommé par
la skill, non renuméroté.

## Conséquences

- La skill peut localiser et corriger une erreur sans parser de texte FR.
- Périmètre tenu : seules les **erreurs de compilation** sont structurées ; les rapports `verify`/`check`
  gardent leur forme (alignée sur le même style JSON), pas de scope-creep.
- `col` n'est fiable que pour les cellules de table ; pour les statements mono-ligne, `line` suffit
  (on ne promet pas une colonne fausse — honnêteté).

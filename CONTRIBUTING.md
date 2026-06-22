# Contribuer à feelc

feelc est un moteur de règles métier (DMN/FEEL) compilé vers un moteur **déterministe** en Go.
Principe directeur : **jamais conformer/prétendre en silence** — tout ce qui est hors périmètre
échoue franchement (ou est signalé en dégradation honnête), jamais accepté-puis-mal-interprété.

## Prérequis & build

- **Go 1.23+** (cf. `go.mod` et les workflows CI/release).
- Le parseur FEEL est un **fork vendorisé** sous `third_party/feel`, épinglé via un `replace` dans
  `go.mod` (exporte `FunCall.Args`, corrige un DoS parseur — cf. [ADR 0004 §1](docs/adr/0004-deferrals.md)).
  Ne pas le « dé-vendoriser ».

```sh
go build ./...
go vet ./...
go test -race ./...                 # tout doit être vert
go test -tags smt ./internal/...    # backend SMT optionnel (cf. ADR 0007 ; requiert z3 pour une preuve)
```

Le sous-module `spike/` (Tranche 0, jetable) a son propre `go.mod` ; il n'est pas dans `./...`.

## Discipline

- **TDD** : test rouge d'abord, puis le minimum pour le vert ; refactor à vert.
- **`go test -race ./...` + `go vet` verts** avant tout commit.
- **Déterminisme** : aucune source d'indéterminisme dans le chemin de décision. Les **goldens**
  (`internal/engine/golden_test.go`) sont rejoués en CI sur **amd64 + arm64** (preuve bit-à-bit).
  Régénération : `FEELC_REGEN_GOLDEN=1 go test ./internal/engine -run Golden`.
- **Pivots** (modifs à sérialiser, jamais paralléliser) : `internal/ir/match.go` (source unique
  VM+verify), `internal/compiler/lower_expr.go` (point d'extension du lowering), le codec
  `internal/ir/codec.go` (toute modif de struct change `ir.Hash` → régénérer les goldens).

## Commits & release

- **Conventional Commits** (`feat:`, `fix:`, `ci:`, `docs:`, `test:`…) : consommés par
  semantic-release. Un `feat:`/`fix:` poussé sur `main` déclenche une release (goreleaser publie les
  binaires multi-OS/arch). Les commits non-release (`ci:`, `docs:`, `test:`…) ne publient pas.
- Terminer les messages par : `Co-Authored-By: ...` si pertinent.

## ADR

Les décisions d'architecture sont dans `docs/adr/` (numérotation : 0001 frontend FEEL, 0002
décimal, 0003 null/erreur, 0004 reports, 0005 erreurs structurées, 0006 sérialisation IR, 0007
backend SMT). Toute décision structurante ajoute/au met à jour un ADR (l'éthique du projet
l'exige : un report doit être documenté, pas masqué).

## La skill d'autoring (`skill/`)

La skill `feelc-rules` vit dans le **sous-dossier `skill/`** du dépôt (pas à la racine). Un
`npx skills add maxgfr/feelc` **nu ne suffit donc PAS** : il cible la racine, qui ne contient pas de
`SKILL.md`. Utiliser la **tree-URL** pointant le sous-dossier :

```sh
npx skills add https://github.com/maxgfr/feelc/tree/main/skill
```

La skill ne décide jamais d'un résultat « de tête » : le binaire `feelc` (compile / verify / run /
check / explain) est l'**oracle déterministe**.

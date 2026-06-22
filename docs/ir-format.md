# Format de l'IR et sérialisation `.ir.bin`

L'**IR** (`internal/ir`) est le modèle compilé, immuable, que la VM exécute et que la vérification
analyse. Trois couches : (1) graphe de décisions (DRG) ; (2) tables normalisées en `CellTest`
(forme géométrique analysable) ; (3) bytecode plat par expression/cellule Op=Prog.

## Sérialisation canonique (`feelc compile -o model.ir.bin`)

Codec : `internal/ir/codec.go` ([ADR 0006](adr/0006-ir-serialization.md)). **Manuel,
length-prefixed, big-endian.** `gob` est proscrit (non canonique → incompatible avec le
déterminisme bit-à-bit, thèse du projet).

### En-tête

```
magic  : 4 octets  "FLIR"
version: uint16     (1)
```

`ir.IsEncoded(b)` teste le magic → le CLI distingue une source `.rules` d'un `.ir.bin` sans se
fier à l'extension. `run` / `verify` / `check` / `explain` acceptent les deux.

### Règles d'encodage

- entiers : big-endian (`uint8`/`uint16`/`uint32`) ;
- chaînes / octets : `uint32` longueur + octets ;
- booléens : 1 octet ;
- **maps** (`Inputs`, `Domains`, `context`) : émises en **ordre de clé trié** (déterminisme) ;
- **décimaux** : via `MarshalText` (texte **exact**, sans `Reduce`) → aucune perte, arch-indépendant ;
- pointeurs (`Table`, `Expr`, `Prog`) : 1 octet de présence puis le contenu.

### Robustesse (blob non fiable)

`Decode` ingère des `.ir.bin` arbitraires : toute longueur est **bornée aux octets restants**
(`count()`, pas de `make` géant → pas d'OOM) et la **profondeur de récursion** est plafonnée
(`maxDecodeDepth`, pas de débordement de pile). Tout dépassement échoue franchement.

## Hash du modèle (`ir.Hash`)

`Hash(cm) = sha256(Encode(cm))` : identité **canonique du modèle compilé** (pas du texte source).
`loader.Compile` l'expose en hex (champ `hash` du service, goldens de déterminisme). **Breaking
voulu** : deux sources qui compilent vers le même IR partagent le hash.

## Positions source

`CellTest.Src/Line`, `Rule.Line/OutputSrc`, `Decision.ExprSrc/Line` portent la trace source
(justification `feelc explain`). Un `.ir.bin` chargé conserve ces champs (ils sont sérialisés) ;
s'ils sont absents (0/""), `explain` dégrade honnêtement.

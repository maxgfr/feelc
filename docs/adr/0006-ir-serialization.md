# ADR 0006 — Sérialisation canonique de l'IR + hash du modèle compilé

- **Statut** : accepté (2026-06-22)
- **Décideurs** : maxgfr

## Contexte

feelc vend du **déterminisme bit-à-bit inter-plateforme**. Il manquait (1) un format de
distribution du modèle **déjà compilé** (`.ir.bin`) pour exécuter sans re-parser/re-compiler,
et (2) une **identité canonique** du modèle compilé pour figer des goldens de non-régression
(Tranche 19). Le hash du service était jusqu'ici `sha256(source)` : sensible au formatage du
texte, pas à la sémantique.

## Décision

Encodage **manuel, length-prefixed big-endian** dans `internal/ir/codec.go` :

- `Encode(cm) ([]byte, error)` / `Decode([]byte) (*CompiledModel, error)` / `Hash(cm) ([32]byte, error)`.
- En-tête `magic ("FLIR") + version (uint16)`. `IsEncoded(b)` teste le magic (le CLI distingue
  ainsi `.rules` d'un `.ir.bin` sans se fier à l'extension).
- **gob proscrit** : non canonique (ordre des champs/maps, drift de version) — incompatible avec
  l'exigence de déterminisme.
- Les **maps** (`Inputs`, `Domains`, `context`) sont émises en **ordre de clé trié**.
- Les **décimaux** passent par `MarshalText` : texte **exact**, **sans `Reduce`** (aucune perte
  de précision ni d'échelle), arch-indépendant.

`loader.Compile` migre vers `hex(ir.Hash(cm))` : l'identité reflète désormais l'**IR**, pas le
texte. Deux sources distinctes qui compilent vers le même IR partagent le hash (**breaking voulu** :
aucun test ne fige l'ancien hash de source).

CLI : `feelc compile --rules x.rules -o x.ir.bin` (affiche taille + hash) ; `run`/`verify`/`check`
acceptent indifféremment une source ou un `.ir.bin`.

## Conséquences

- Distribution/exécution d'un modèle compilé sans la chaîne de parsing.
- Base des goldens déterministes (ADR/Tranche 19) : `modelHash` rejouable amd64 + arm64.
- Round-trip prouvé stable (`Encode→Decode→Encode` identique bit-à-bit) ; magic invalide rejeté
  (jamais conformer en silence).
- Le `.ir.bin` ne porte **pas** les positions source (`Src`/`Line`/`Col`) : `explain` sur un binaire
  dégrade honnêtement (positions absentes).

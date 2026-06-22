# ADR 0002 — Arithmétique décimale : cockroachdb/apd vs int128 maison

- **Statut** : accepté (Tranche 0, 2026-06-22)
- **Décideurs** : maxgfr

## Contexte

feelc DOIT être **déterministe bit-à-bit inter-plateforme** : c'est la thèse centrale du produit
(décisions reproductibles, auditables, rejouables). Les domaines crédit/assurance manipulent des montants
et des taux (`dti = monthly_debt / (annual_income / 12)`, comparé à `0.43`) où l'arithmétique binaire
(`float64`, `big.Float`) introduit des erreurs de représentation (`0.1 + 0.2 != 0.3`) et un arrondi
non maîtrisé. Il faut donc un **décimal exact** avec un arrondi **HALF_EVEN** (banker's rounding) figé.

Le plan « complet » évoquait un décimal **int128 maison** (mantisse int128 + échelle). La revue adverse
a classé cela comme un **piège de plusieurs semaines** (Go n'a pas d'int128 natif ; add/sub/mul/div +
arrondi + parsing/format corrects = un projet en soi, source de bugs de déterminisme subtils).

## Évaluation empirique (spike `spike/decimal_test.go`)

`github.com/cockroachdb/apd/v3@v3.2.3` (Apache-2.0, utilisé en prod par CockroachDB) vérifié :
- **Exactitude** : `0.1 + 0.2 == 0.3` exact ; `1500 / (60000/12) == 0.3` exact (cas dti crédit). ✅
- **HALF_EVEN** : `2.5→2`, `3.5→4`, `2.125→2.12`, `2.135→2.14` (à 2 décimales). ✅

## Décision

1. **Utiliser `cockroachdb/apd/v3`** comme moteur décimal de feelc dès le v1. Contexte figé :
   précision suffisante (≥ 34 chiffres, type Decimal128) et `Rounding = RoundHalfEven` par défaut,
   l'arrondi du modèle (`rounding: half_even`) étant stocké dans l'IR.
2. Le wrapper vit dans `internal/decimal` : il expose le strict nécessaire (parse depuis le littéral
   source, +, -, *, /, comparaison, quantize/arrondi) et **fige le contexte** pour garantir le
   déterminisme — aucune dépendance à un état global mutable.
3. L'**int128 inline maison reste une micro-optimisation différée**, à n'envisager qu'**après** des
   benchmarks (`testing.B -benchmem`) montrant que apd est le goulot du hot path. Pas une décision d'archi v1.

## Conséquences

- Déterminisme et exactitude **acquis immédiatement**, sans semaines de code numérique fragile.
- Une dépendance Apache-2.0 de plus (compatible avec la licence Apache-2.0 de feelc).
- `Value` de la VM portera un décimal apd (ou une vue compacte de celui-ci) ; le coût d'allocation
  d'apd dans le hot path sera **mesuré** en Tranche 4/5 et optimisé si nécessaire (pooling, ou int128).

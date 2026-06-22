# Exemple de référence — Éligibilité / scoring de crédit

L'exemple canonique d'un BRMS (le « hello world » d'IBM ODM). Il exerce les briques clés de feelc :
décisions **liées** (DRG), **expression FEEL** intermédiaire, **table** avec ranges et comparaisons,
hit policy **FIRST**, ligne **default**, et sortie **context** multi-champs.

## Entrées (Input Data)

| Nom             | Type   | Domaine        |
|-----------------|--------|----------------|
| `credit_score`  | number | `[300..850]`   |
| `annual_income` | number | `>= 0`         |
| `monthly_debt`  | number | `>= 0`         |
| `age`           | number | `[0..120]`     |

## Décisions

1. **`dti`** (number) — taux d'endettement mensuel : `monthly_debt / (annual_income / 12)`.
2. **`eligibility`** (context `{eligible, reason}`) — table FIRST sur `credit_score`, `dti`, `age`.

## Règles métier (ordre = priorité)

1. score `< 580` → refus « score insuffisant »
2. `dti > 0.43` → refus « endettement trop élevé »
3. `age < 18` → refus « mineur »
4. score `[580..680)` et `dti <= 0.43` et `age >= 18` → accord « approuvé sous conditions »
5. score `>= 680` et `dti <= 0.43` et `age >= 18` → accord « approuvé »
6. sinon (`default`) → refus « non couvert »

# Exemple de référence — Tarification assurance

Exerce **COLLECT C+ (somme)** d'éléments de risque cumulables et un **DRG** (la prime dépend
de la surcharge calculée).

## Entrées
- `age` (number, `[18..100]`), `region` (string, `{urban, suburban, rural}`),
  `claims` (number, `>= 0`), `base_premium` (number, `>= 0`).

## Décisions
1. **`surcharge`** (number, `collect sum`) — somme des surcharges déclenchées :
   - âge `[18..25)` → +300 ; region `urban` → +150 ; `claims >= 3` → +500 ; âge `>= 70` → +200.
2. **`premium`** (number) — `base_premium + surcharge`.

## Exemples
- age 22 / urban / 4 sinistres / base 1000 → surcharge 950 → **premium 1950**.
- age 40 / rural / 0 sinistre / base 800 → surcharge 0 → **premium 800**.
- age 72 / urban / 0 sinistre / base 1000 → surcharge 350 → **premium 1350**.

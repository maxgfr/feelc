# Exemple de référence — Promotions e-commerce

Exerce **COLLECT C> (max)** : plusieurs remises peuvent s'appliquer, on retient la plus forte.
Règles qui changent souvent → bon argument pour le hot-reload (Tranche 5).

## Entrées
- `cart_total` (number, `>= 0`), `is_member` (boolean), `promo_code` (string).

## Décision
- **`discount_pct`** (number, `collect max`) — meilleure remise applicable :
  - `cart_total >= 50` → 5 % ; `cart_total >= 100` → 10 % ; membre → 8 % ;
    code `WELCOME10` → 10 % ; code `BIG20` → 20 %.

## Exemples
- panier 120 / membre / `BIG20` → max(5, 10, 8, 20) = **20**.
- panier 60 / non-membre / code inconnu → **5**.
- panier 30 / non-membre / code inconnu → aucune remise → **null**.

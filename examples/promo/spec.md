# Reference example — E-commerce promotions

Exercises **COLLECT C> (max)**: several discounts may apply, the highest one is kept.
Rules that change often → a good argument for hot-reload (Slice 5).

## Inputs
- `cart_total` (number, `>= 0`), `is_member` (boolean), `promo_code` (string).

## Decision
- **`discount_pct`** (number, `collect max`) — best applicable discount:
  - `cart_total >= 50` → 5 % ; `cart_total >= 100` → 10 % ; member → 8 % ;
    code `WELCOME10` → 10 % ; code `BIG20` → 20 %.

## Examples
- cart 120 / member / `BIG20` → max(5, 10, 8, 20) = **20**.
- cart 60 / non-member / unknown code → **5**.
- cart 30 / non-member / unknown code → no discount → **null**.

# Gabarits — les 4 modèles de référence

Le dépôt `feelc` fournit 4 exemples complets dans `examples/` (chacun avec un `spec.md`). Réutilise-les
comme patrons. Résumé des techniques qu'ils illustrent :

## Crédit (`examples/credit`) — FIRST + DRG + context
Décision intermédiaire `dti` (expression), décision finale `eligibility` (table FIRST, ranges +
comparaisons, sortie context `{eligible, reason}`, ligne `default`). FIRST = refus prioritaires d'abord.

```
decision dti : number = monthly_debt / (annual_income / 12)
decision eligibility : Eligibility {
  needs: credit_score, dti, age
  hit: first
     < 580      | -       | -     => false | "score insuffisant"
     [580..680) | <= 0.43 | >= 18 => true  | "approuvé sous conditions"
     >= 680     | <= 0.43 | >= 18 => true  | "approuvé"
     default    |         |       => false | "non couvert"
}
```

## Assurance (`examples/insurance`) — COLLECT sum + DRG
Surcharges de risque **cumulables** (`hit: collect sum`), puis `premium = base_premium + surcharge`.

## Aides (`examples/benefits`) — COLLECT (liste) + booléen
Aides cumulables → `hit: collect` renvoie la **liste** des aides accordées ; condition booléenne `true`.

## Promos (`examples/promo`) — COLLECT max
Plusieurs remises applicables, on garde la plus forte → `hit: collect max`.

## Conseils transversaux
- Domaines bornés sur les entrées numériques → complétude vérifiable.
- Une décision finale par « question métier » ; factorise les calculs en décisions intermédiaires.
- Teste chaque exemple : `run --decision <nom> --input '{…}'` puis `verify`.

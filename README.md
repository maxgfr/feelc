# feelc

> Un pseudo-langage de règles métier (DMN/FEEL) **compilé en Go**, dans l'esprit d'IBM ODM/ILOG —
> avec un angle distinctif : **l'IA aide à rédiger et expliquer les règles, mais à l'exécution tout
> est 100 % déterministe, reproductible et auditable** (aucun LLM dans le cœur).

## Pourquoi

Les moteurs de règles classiques opposent *lisibilité métier* et *exécution fiable*. `feelc` réconcilie
les deux :

- **L'IA écrit, le moteur exécute.** On rédige les règles dans un DSL `.rules` lisible (paradigme DMN :
  un graphe de décisions, chacune une table de décision, expressions en FEEL). Un LLM peut le générer
  nativement. Le compilateur Go le transforme en IR typé et vérifié, exécuté par une petite VM déterministe.
- **Vérification formelle.** `feelc verify` prouve la **complétude** (aucun cas non couvert),
  l'**absence de conflits**, et détecte **règles mortes / redondances** — avec des contre-exemples concrets.
- **Hot-reload.** Les règles sont des *données* : on les met à jour à chaud, sans recompiler le binaire.
- **Auditable.** Chaque décision est rejouable (hash du modèle + trace d'explication citant la source).

## Statut

🚧 En construction (voir le plan de développement par tranches). Le cœur (DSL → IR → VM) et la
vérification arrivent en premier ; service hot-reload, interop DMN XML, gate sémantique et skill suivent.

## Exemple

```
model "credit" {
  rounding: half_even
}

input credit_score  : number in [300..850]
input annual_income : number >= 0
input monthly_debt  : number >= 0
input age           : number in [0..120]

decision dti : number = monthly_debt / (annual_income / 12)

decision eligibility : Eligibility {
  needs: credit_score, dti, age
  hit: first
  #  credit_score | dti     | age   => eligible | reason
     < 580        | -       | -     => false    | "score insuffisant"
     -            | > 0.43  | -     => false    | "endettement trop élevé"
     -            | -       | < 18  => false    | "mineur"
     [580..680)   | <= 0.43 | >= 18 => true     | "approuvé sous conditions"
     >= 680       | <= 0.43 | >= 18 => true     | "approuvé"
     default      |         |       => false    | "non couvert"
}
```

## Licence

Apache-2.0. Voir [LICENSE](./LICENSE).

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

## Commandes

```sh
feelc run    --rules m.rules --decision <nom> --input '{…}' [--json]   # évaluer une décision
feelc verify --rules m.rules [--json]                                  # vérif formelle (trous/conflits)
feelc check  --rules m.rules --claims claims.json [--json]             # gate sémantique NL↔règle
feelc import --in modele.dmn [-o m.rules]                              # importer du DMN XML
feelc serve  --rules m.rules [--addr :8080] [--watch] [--strict]       # service HTTP + hot-reload
```

## Statut

Cœur **opérationnel** : langage → compilateur → IR → VM déterministe (décimal exact), 7 hit policies,
**vérification formelle** (complétude/conflits/règles mortes avec contre-exemples), **service HTTP +
hot-reload**, **gate sémantique** (`check`), **import DMN XML**. 4 exemples de référence vérifiés.
Skill d'autoring : [maxgfr/feelc-rules](https://github.com/maxgfr/feelc-rules).
Reportés (ADR 0004) : BKM paramétré, extension SMT/Z3.

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

# Lire `feelc verify`

```sh
node scripts/feelc-skill.mjs verify --rules model.rules --json
```

Sortie JSON : `{ "findings": [ { decision, kind, severity, message, witness?, rules? }, … ] }`.
La commande **exit 1** s'il existe au moins un finding `severity: "error"` (bloqueur), 0 sinon.

## Sévérités

- **`error` (bloqueur)** — à corriger avant de considérer le modèle « buildable » :
  - `gap` : un cas n'est couvert par **aucune** règle (table single-hit, sans `default`).
    `witness` donne un **contre-exemple concret** (ex. `{"n":"45"}`) → ajoute une règle ou un `default`.
  - `conflict` : sous `unique`, deux règles se chevauchent ; sous `any`, elles donnent des sorties
    différentes. `witness` + `rules` pointent le problème.
- **`warning`** (à signaler, pas toujours à corriger) :
  - `gap` rattrapé par `default` ; `dead-rule` (règle jamais atteignable, ou masquée par une règle
    antérieure sous `first`). Souvent le signe d'une règle redondante ou mal ordonnée.
- **`info`** :
  - `unreachable-default` : la ligne `default` n'est jamais utilisée (les règles couvrent déjà tout).
  - `not-verifiable` : table non prouvable géométriquement (cellule `Op=Prog`, c.-à-d. comparaison à
    une autre colonne, ou grille trop grande). Honnête : **non prouvé**, jamais « conforme » en silence.

## Critère d'arrêt

> **Buildable** = 0 bloqueur (`error`). **Convergent** = `run` reproduit les cas de référence.

Ne supprime pas une règle juste pour faire taire un `warning` : comprends d'abord *pourquoi* il
apparaît (souvent un chevauchement ou un ordre de lignes à revoir). Les `error` doivent disparaître ;
les `warning`/`info` se **commentent** à l'utilisateur.

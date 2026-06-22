# Pièges classiques à éviter

| Piège | Symptôme `verify` | Correctif |
|---|---|---|
| **Table single-hit incomplète** (pas de `default`, domaine non couvert) | `gap` (error) + contre-exemple | Ajoute une règle pour la zone manquante, ou une ligne `default` assumée |
| **Chevauchement sous `unique`** | `conflict` (error) | Resserre les conditions, ou passe en `first`/`priority` si l'ordre/priorité est voulu |
| **`any` avec sorties divergentes** | `conflict` (error) | Aligne les sorties, ou change de hit policy |
| **Règle masquée** (une règle antérieure couvre déjà tous ses cas en `first`) | `dead-rule` (warning) | Réordonne, ou supprime la règle redondante |
| **`default` inutile** | `unreachable-default` (info) | OK (filet de sécurité) ou retire-le si les règles sont prouvées complètes |
| **Fonction FEEL / `if-then-else` dans une expression** | erreur de compilation | Hors sous-ensemble v2 — reformule (table, ou décision intermédiaire) |
| **`sum([...])` pour additionner des cas** | erreur de compilation | Utilise la hit policy `collect sum`, pas une fonction |
| **Sortie calculée dans une cellule** | erreur de compilation | Les sorties de table sont des littéraux ; calcule via une décision `= <expr>` |
| **Comparer une colonne à une autre** (`> autre_colonne`) | `not-verifiable` (info) | OK mais la complétude n'est pas prouvée sur cette table (cellule Op=Prog) |
| **`not(< 18)`** | erreur de compilation | Remplace par la condition complémentaire (`>= 18`) |

## Déterminisme
feelc est déterministe par construction : pas d'horloge, pas d'aléa, décimal exact. Si tu veux une
règle « selon la date », passe la date/heure comme **entrée** du modèle (le moteur ne lit jamais
l'horloge lui-même).

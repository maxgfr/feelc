# Exemple de référence — Éligibilité aides / prestations

Exerce **COLLECT brut (liste)**, des conditions imbriquées et un test **booléen**. Les aides
sont cumulables ; la décision renvoie la liste des aides accordées.

## Entrées
- `income` (number, `>= 0`), `children` (number, `[0..15]`), `is_student` (boolean).

## Décision
- **`aids`** (string, `collect`) — liste des aides :
  - `income < 1500` → `"housing"` ; `children >= 1` → `"family"` ;
    `income < 1000` et `is_student = true` → `"student_grant"`.

## Exemples
- income 900 / 2 enfants / étudiant → `["housing", "family", "student_grant"]`.
- income 2000 / 0 enfant / non-étudiant → `[]`.
- income 1200 / 1 enfant / non-étudiant → `["housing", "family"]`.

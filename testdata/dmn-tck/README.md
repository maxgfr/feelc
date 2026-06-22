# Sous-ensemble DMN TCK (fixtures)

Fixtures au **format officiel du DMN TCK** (`<testCases>`, namespace
`http://www.omg.org/spec/DMN/20160719/testcase`) : une paire `<modèle>.dmn` + un ou plusieurs
fichiers `*-test-*.xml`. Exécutées par `feelc tck --suite testdata/dmn-tck` (et par
`internal/tck`).

## Provenance

Ces fichiers sont **rédigés à la main** pour rester *self-contained* et déterministes (pas de
téléchargement réseau). Ils miment fidèlement le schéma du
[DMN TCK officiel](https://github.com/dmn-tck/tck) (Apache-2.0). Le harnais `feelc tck` fonctionne
aussi tel quel sur un **checkout réel du TCK** : `feelc tck --suite <path-vers-tck/TestCases>`.

## Cas retenus

- `grade/` — table `FIRST` scalaire (score → F/B/A). `grade-test-01.xml` : 3 cas **passants**.
  `grade-test-02-skip.xml` : 1 cas **skippé** (valeur attendue de type `date`, hors sous-ensemble).

## Familles volontairement absentes (hors sous-ensemble v2, seraient SKIPPÉES)

- types temporels (`date`, `time`, `dateTime`, `duration`) et `function` ;
- DRG multi-décisions dont le câblage `informationRequirement`/`requiredDecision` n'est pas importé ;
- built-ins multi-arguments (cf. ADR 0004 §3).

Le rapport `feelc tck` distingue toujours `passed` / `failed` / `skipped` (avec raison) : la
conformité = `passed / (passed+failed)`, les skips n'y comptent pas (couverture honnête).

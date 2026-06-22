# Sous-ensemble FEEL supporté

feelc réutilise le parseur `pbinitiative/feel` (forké et vendorisé sous `third_party/feel`, cf.
[ADR 0001](adr/0001-feel-frontend.md) et [ADR 0004 §1](adr/0004-deferrals.md)) mais **n'exécute
pas** son évaluateur : feelc compile vers son propre bytecode déterministe (décimaux exacts, cf.
[ADR 0002](adr/0002-decimal.md)). Le périmètre est volontairement borné ; **tout le reste échoue
franchement** (le compilateur est le gardien du périmètre).

## Expressions (literal-expression et cellules Op=Prog)

Supporté (`internal/compiler/lower_expr.go`) :

- **littéraux** : nombres (décimaux exacts), chaînes, booléens ;
- **variables** : noms d'inputs / de décisions amont ; `?` = valeur de la colonne courante
  (cellules de table uniquement) ;
- **arithmétique** : `+ - * /` (décimale exacte, division par zéro = erreur) ;
- **comparaisons** : `= != < <= > >=` ;
- **logique** : `and`, `or`, `not(x)` ;
- **conditionnel** : `if c then a else b` (compilé en sauts `OpJmpFalse`/`OpJmp`) ;
- **built-ins mono-arg purs** : `floor(x)`, `ceiling(x)`, `round(x)` (arrondi HALF_EVEN, déterministe) ;
- **invocation de BKM** : `nom(a, b)` — **inlinée** à la compilation (substitution AST, zéro frame
  d'appel ; récursion auto/mutuelle détectée et **rejetée**).

## Cellules de table (unary tests)

`-` (any), littéral (égalité), `< x` / `<= x` / `> x` / `>= x`, intervalle `[a..b]` / `(a..b)` /
`[a..b)`, ensemble `a, b, c`, négation `not(<test>)` (reste **géométrique**, donc analysable par la
vérification), et expression libre (référence `?`/autres colonnes → *Op=Prog*, non géométrique).

## Hors périmètre (échec franc)

- built-ins **multi-arguments** : `round(x, n)`, `substring(s, i, n)`, etc. ([ADR 0004 §3](adr/0004-deferrals.md)) ;
- `for` / `some` / `every`, listes/filtres, fonctions d'ordre supérieur, `function(...)` ;
- types **temporels** (`date`, `time`, `dateTime`, `duration`) ;
- `**` (puissance), opérateurs non listés ;
- `?` dans une expression **literal-expression** (réservé aux cellules de table) ;
- arguments **nommés** (kwargs) dans une invocation de BKM.

## Déterminisme

Contexte décimal **figé** (precision 34 / HALF_EVEN), aucune source d'indéterminisme dans le
chemin de décision. Les sorties sont rejouables bit-à-bit inter-plateforme (goldens CI amd64+arm64).
La vérification formelle ([verify](../README.md)) prouve complétude/conflits/subsumption sur la
couche géométrique ; les cellules Op=Prog sont signalées `not-verifiable` (ou routées vers SMT
sous `-tags smt`, [ADR 0007](adr/0007-smt-backend.md)).

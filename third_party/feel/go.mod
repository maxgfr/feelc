// Fork vendorisé de github.com/pbinitiative/feel@v1.0.6 (MIT). Deux divergences amont :
//  1. FunCall.Args EXPORTÉ (`[]FunCallArg{Name, Arg}`) — feelc lit les arguments d'une
//     invocation `nom(a, b)` (inlining BKM, ADR 0004 §1).
//  2. Correctif DoS : `singleElement` consomme désormais le token `?` (parser.go). L'amont le
//     laissait non consommé → `Parse` bouclait à l'infini sur un `?` explicite (OOM ~100 Go).
// Épinglé via `replace` dans le go.mod racine. Les fichiers *_test.go et sous-paquets
// (cli/, cmd/, tests/) ne sont pas vendorisés (non importés par feelc → testify hors graphe).
module github.com/pbinitiative/feel

go 1.23

require (
	github.com/google/go-cmp v0.5.9
	github.com/mitchellh/mapstructure v1.5.0
)

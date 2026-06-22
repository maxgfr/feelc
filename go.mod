module github.com/maxgfr/feelc

go 1.23

require (
	github.com/cockroachdb/apd/v3 v3.2.3
	github.com/fsnotify/fsnotify v1.10.1
	github.com/pbinitiative/feel v1.0.6
)

require (
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
)

// Fork vendorisé : exporte FunCall.Args (FunCallArg{Name, Arg}) pour lire les arguments
// d'invocation (inlining BKM, ADR 0004 §1). Épinglé localement, pas de drift amont silencieux.
replace github.com/pbinitiative/feel => ./third_party/feel

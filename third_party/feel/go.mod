// Vendored fork of github.com/pbinitiative/feel@v1.0.6 (MIT). Two divergences from upstream:
//  1. FunCall.Args EXPORTED (`[]FunCallArg{Name, Arg}`) — feelc reads the arguments of an
//     invocation `name(a, b)` (BKM inlining, ADR 0004 §1).
//  2. DoS fix: `singleElement` now consumes the `?` token (parser.go). Upstream left it
//     unconsumed → `Parse` looped forever on an explicit `?` (OOM ~100 GB).
// Pinned via `replace` in the root go.mod. The *_test.go files and subpackages
// (cli/, cmd/, tests/) are not vendored (not imported by feelc → testify out of the graph).
module github.com/pbinitiative/feel

go 1.23

require (
	github.com/google/go-cmp v0.5.9
	github.com/mitchellh/mapstructure v1.5.0
)

//go:build !(js && wasm)

// Command feelc-wasm builds the deterministic feelc engine to WebAssembly for the in-browser
// playground (GOOS=js GOARCH=wasm). This stub keeps the package buildable on host platforms so
// `go build/vet/test ./...` succeed; the real entrypoint lives in main_wasm.go (//go:build js && wasm).
package main

func main() {}

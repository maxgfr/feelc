package service

import (
	"embed"
	"io/fs"
)

// webEmbed bundles the zero-dependency authoring UI into the binary (single-binary distribution).
// Served at `/` only when `feelc serve --ui` sets Server.EnableUI.
//
//go:embed web
var webEmbed embed.FS

// uiFS returns the UI asset tree rooted so that index.html is served at `/`.
func uiFS() fs.FS {
	sub, err := fs.Sub(webEmbed, "web")
	if err != nil {
		panic(err) // the embed is resolved at build time; this cannot fail at runtime
	}
	return sub
}

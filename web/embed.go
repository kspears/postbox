// Package web holds the built frontend. `just build` fills dist; a checkout that has not built
// it yet still compiles, because dist/.gitkeep is tracked. Every build empties dist, and
// public/.gitkeep is what puts the tracked file back.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built frontend, rooted at index.html.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a constant, valid path
	}
	return sub
}

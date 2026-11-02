// Package web embeds the compiled React panel into the Go binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the Vite build output rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// fs.Sub only fails for invalid patterns; "dist" is a constant.
		panic(err)
	}
	return sub
}

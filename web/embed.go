// Package web embeds the built frontend so that a single gintrack binary can
// serve the UI with no external assets.
//
// Only this file is Go: everything else under web/ is the React + Vite
// application. The bundle is produced by `make web` into web/dist, which is
// git-ignored except for the .gitkeep that keeps the embed pattern satisfiable
// before the first build.
package web

import (
	"embed"
	"fmt"
	"io/fs"
)

// Dist holds the built web application. It is empty until `make web` has run,
// in which case the server falls back to a page explaining how to build it.
//
//go:embed all:dist
var Dist embed.FS

// DistFS returns the embedded bundle rooted at web/dist, so that "index.html"
// resolves without the "dist/" prefix.
func DistFS() (fs.FS, error) {
	sub, err := fs.Sub(Dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("open the embedded web bundle: %w", err)
	}
	return sub, nil
}

// Built reports whether the embedded bundle contains an index.html, that is,
// whether the frontend was built before the binary was compiled.
func Built() bool {
	sub, err := DistFS()
	if err != nil {
		return false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return false
	}
	return true
}

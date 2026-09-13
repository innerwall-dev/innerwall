//go:build !noconsole

// Package ui carries the built operator console into the control-plane
// binary. The console is compiled by Vite into dist/ and embedded here so
// the control plane ships as one artifact with no separate frontend
// service (ADR-0008); the operator listener serves it at every path
// outside the API prefix (ADR-0021). The console talks only to the public
// REST surface (ADR-0007).
//
// The noconsole build tag swaps this file for a stub that embeds nothing,
// so Go-only iteration never needs the Node toolchain.
package ui

import (
	"embed"
	"io/fs"
)

// assets holds the built console. Before `make console` has run, dist/
// contains only a placeholder so the control plane still compiles from a
// fresh checkout; the listener notices the missing entry point and
// answers that the console is not served by this build.
//
//go:embed all:dist
var assets embed.FS

// Console returns the built console rooted at its entry point. It never
// returns nil in this build; whether the tree holds a console is the
// listener's check.
func Console() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		// dist is a literal in the embed directive above; the sub-tree
		// cannot be missing.
		panic(err)
	}
	return sub
}

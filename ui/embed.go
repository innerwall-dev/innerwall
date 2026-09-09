// Package ui carries the built single-page application into the control-plane
// binary. The SPA is compiled by Vite into dist/ and embedded here so that the
// control plane ships as one artifact with no separate frontend service
// (ADR-0008). The UI talks only to the public REST façade (ADR-0007).
package ui

import "embed"

// Assets holds the built SPA. Before `make ui` has run, dist/ contains only a
// placeholder so that the control plane still compiles from a fresh checkout.
//
//go:embed all:dist
var Assets embed.FS

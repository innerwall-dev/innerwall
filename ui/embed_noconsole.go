//go:build noconsole

// Package ui carries the built operator console into the control-plane
// binary. This is the noconsole build: nothing is embedded, and the
// listener answers every console path with the marked mount-point
// problem. It exists so Go-only iteration never needs the Node toolchain.
package ui

import "io/fs"

// Console returns nil: this build carries no console.
func Console() fs.FS { return nil }

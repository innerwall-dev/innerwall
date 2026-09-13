//go:build !noconsole

package ui

import (
	"errors"
	"io/fs"
	"testing"
)

// The default build always carries a tree, even a placeholder one; the
// listener decides from its contents whether a console is served.
func TestConsoleIsATree(t *testing.T) {
	tree := Console()
	if tree == nil {
		t.Fatal("Console() = nil in the default build")
	}
	if _, err := fs.ReadDir(tree, "."); err != nil {
		t.Fatalf("reading the console tree: %v", err)
	}
	if _, err := fs.Stat(tree, "index.html"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat index.html: %v", err)
	}
}

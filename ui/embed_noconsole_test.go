//go:build noconsole

package ui

import "testing"

// The noconsole build carries nothing at all.
func TestConsoleIsAbsent(t *testing.T) {
	if Console() != nil {
		t.Fatal("Console() != nil in the noconsole build")
	}
}

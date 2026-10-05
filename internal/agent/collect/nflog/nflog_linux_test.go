//go:build linux

package nflog

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
)

// TestOverrunIsRecoveredAndRecorded checks the log source's overrun path
// without netlink: the kernel's ENOBUFS, however the library wraps it, is
// counted and recorded as an overrun gap from the last event read to now,
// and reading carries on; any other error is not an overrun.
func TestOverrunIsRecoveredAndRecorded(t *testing.T) {
	gaps := &collect.Gaps{}
	s := &Source{Group: 7, Gaps: gaps}
	last := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	now := last.Add(2 * time.Second)

	wrapped := fmt.Errorf("receive: %w", os.NewSyscallError("recvmsg", unix.ENOBUFS))
	if !s.overran(wrapped, last, now) {
		t.Fatal("ENOBUFS not recognized as an overrun")
	}
	if got := gaps.Overruns(); got != 1 {
		t.Fatalf("overruns = %d, want 1", got)
	}
	want := collect.Gap{Kind: collect.GapSourceOverrun, Source: collect.GapNflog, From: last, To: now}
	if got := gaps.Pending(); len(got) != 1 || got[0] != want {
		t.Fatalf("gaps = %+v, want [%+v]", got, want)
	}

	if s.overran(errors.New("netlink receive: bad file descriptor"), last, now) {
		t.Fatal("an ordinary error was taken for an overrun")
	}
	if got := gaps.Overruns(); got != 1 {
		t.Fatalf("overruns after an ordinary error = %d, want 1", got)
	}
}

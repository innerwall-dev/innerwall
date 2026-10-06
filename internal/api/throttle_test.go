package api

import (
	"fmt"
	"testing"
	"time"
)

// TestThrottleBoundsSourcesWithinAWindow is the regression for a table
// that grew without bound while every window was still running: with the
// clock frozen, 5000 distinct sources leave the table at its bound, and
// the oldest windows are the ones that left.
func TestThrottleBoundsSourcesWithinAWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	th := &Throttle{Limit: 2, Now: func() time.Time { return now }}
	for i := range 5000 {
		if ok, _ := th.Allow(fmt.Sprint("src-", i)); !ok {
			t.Fatalf("first attempt from source %d refused", i)
		}
		if th.Sources() > throttleMaxSources {
			t.Fatalf("table holds %d sources after %d arrivals, bound %d", th.Sources(), i+1, throttleMaxSources)
		}
	}
	if th.Sources() != throttleMaxSources {
		t.Fatalf("table holds %d sources, want the bound %d", th.Sources(), throttleMaxSources)
	}
	// The newest source's window is intact: it is refused past its limit.
	newest := fmt.Sprint("src-", 4999)
	th.Allow(newest)
	if ok, _ := th.Allow(newest); ok {
		t.Fatal("newest source's window was evicted")
	}
	// The oldest source was evicted, so it starts a fresh window.
	if ok, _ := th.Allow("src-0"); !ok {
		t.Fatal("oldest source still holds its window")
	}
}

// TestThrottleEvictsElapsedBeforeLive checks the eviction order at the
// bound: windows that elapsed go before any still running, and a source
// whose window restarts becomes the newest.
func TestThrottleEvictsElapsedBeforeLive(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	th := &Throttle{Limit: 1, Window: time.Minute, Now: func() time.Time { return now }}
	// The first half of the table starts at t0, the rest a minute later,
	// so at t0+60s the first half has elapsed and the second has not.
	for i := range throttleMaxSources / 2 {
		th.Allow(fmt.Sprint("early-", i))
	}
	now = now.Add(time.Minute)
	for i := range throttleMaxSources - throttleMaxSources/2 {
		th.Allow(fmt.Sprint("late-", i))
	}
	// A restart moves a source to the back: early-0 begins a new window
	// and must survive the evictions that follow.
	th.Allow("early-0")
	// Exactly as many new sources as elapsed windows remain: they take
	// the elapsed windows' places and nothing running is evicted.
	for i := range throttleMaxSources/2 - 1 {
		th.Allow(fmt.Sprint("new-", i))
	}
	if th.Sources() != throttleMaxSources {
		t.Fatalf("table holds %d sources", th.Sources())
	}
	// Every running late window survived and still refuses a second
	// attempt; early-0's restarted window did too.
	for _, key := range []string{"late-0", fmt.Sprint("late-", throttleMaxSources-throttleMaxSources/2-1), "early-0"} {
		if ok, _ := th.Allow(key); ok {
			t.Fatalf("%s lost its running window", key)
		}
	}
	// One more source finds no elapsed window left and evicts the oldest
	// running one, late-0, which then starts afresh.
	th.Allow("one-more")
	if ok, _ := th.Allow("late-0"); !ok {
		t.Fatal("the oldest running window was not the one evicted")
	}
}

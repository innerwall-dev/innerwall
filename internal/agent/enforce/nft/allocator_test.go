package nft

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

func rulesPolicy(ids ...string) *innerwallv1.WorkloadPolicy {
	p := &innerwallv1.WorkloadPolicy{Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED}
	for _, id := range ids {
		p.InboundRules = append(p.InboundRules, &innerwallv1.ResolvedRule{RuleId: id, Protocol: innerwallv1.Protocol_PROTOCOL_TCP})
	}
	return p
}

// TestAllocatorReusesOnlyAtExhaustion runs an allocation over a region of
// four values: values are handed out in increasing order; a freed value
// keeps naming its rule while unused values remain; once the region is
// used up the value freed longest ago goes first; a value the installed
// policy holds is never reused; and with nothing reusable the allocation
// is refused.
func TestAllocatorReusesOnlyAtExhaustion(t *testing.T) {
	a := newAllocator(4)
	marks, err := a.Allocate(rulesPolicy("r1", "r2"), nil)
	if err != nil || marks["r1"] != 1 || marks["r2"] != 2 {
		t.Fatalf("first allocation = %v, %v", marks, err)
	}
	// r1 leaves; r3 takes an unused value, not r1's.
	installed := rulesPolicy("r1", "r2")
	marks, err = a.Allocate(rulesPolicy("r2", "r3"), installed)
	if err != nil || marks["r2"] != 2 || marks["r3"] != 3 {
		t.Fatalf("second allocation = %v, %v", marks, err)
	}
	if id, ok := a.Rule(1); !ok || id != "r1" {
		t.Fatalf("freed value 1 names %q, %v; want the removed r1", id, ok)
	}
	// r2 leaves; r4 takes the last unused value.
	installed = rulesPolicy("r2", "r3")
	if marks, err = a.Allocate(rulesPolicy("r3", "r4"), installed); err != nil || marks["r4"] != 4 {
		t.Fatalf("third allocation = %v, %v", marks, err)
	}
	// The region is used up: r5 takes value 1, freed longest ago (r1),
	// and r6 takes value 2 (r2, freed next).
	installed = rulesPolicy("r3", "r4")
	if marks, err = a.Allocate(rulesPolicy("r3", "r4", "r5"), installed); err != nil || marks["r5"] != 1 {
		t.Fatalf("allocation at exhaustion = %v, %v", marks, err)
	}
	if id, _ := a.Rule(1); id != "r5" {
		t.Fatalf("reused value 1 names %q", id)
	}
	if _, ok := a.Mark("r1"); ok {
		t.Fatal("r1 still holds a value that was reused")
	}
	installed = rulesPolicy("r3", "r4", "r5")
	if marks, err = a.Allocate(rulesPolicy("r3", "r4", "r5", "r6"), installed); err != nil || marks["r6"] != 2 {
		t.Fatalf("second allocation at exhaustion = %v, %v", marks, err)
	}
	// Every value is held by the incoming policy: refused.
	installed = rulesPolicy("r3", "r4", "r5", "r6")
	if _, err := a.Clone().Allocate(rulesPolicy("r3", "r4", "r5", "r6", "r7"), installed); !errors.Is(err, ErrMarksExhausted) {
		t.Fatalf("allocation past the region = %v", err)
	}
	// A value held by the installed policy is not reusable even when the
	// incoming policy drops its rule: r3's value stays r3's while the
	// kernel may still mark connections with it.
	b := newAllocator(2)
	if _, err := b.Allocate(rulesPolicy("x", "y"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Allocate(rulesPolicy("z"), rulesPolicy("x", "y")); !errors.Is(err, ErrMarksExhausted) {
		t.Fatalf("reused a value the installed policy holds: %v", err)
	}
}

// TestAllocatorPersists checks that the allocation round-trips through
// its file, including freed values, and that a damaged or inconsistent
// file is refused.
func TestAllocatorPersists(t *testing.T) {
	dir := t.TempDir()
	path := MarksPath(dir)
	if a, err := LoadAllocator(path); a != nil || err != nil {
		t.Fatalf("missing file = %v, %v", a, err)
	}
	a := NewAllocator()
	if _, err := a.Allocate(rulesPolicy("r1", "r2", "r3"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Allocate(rulesPolicy("r3", "r4"), rulesPolicy("r1", "r2", "r3")); err != nil {
		t.Fatal(err)
	}
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("marks file stat = %v, %v", info, err)
	}
	b, err := LoadAllocator(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]uint32{"r1": 1, "r2": 2, "r3": 3, "r4": 4} {
		if got, ok := b.Mark(id); !ok || got != want {
			t.Fatalf("loaded %s = %d, %v; want %d", id, got, ok, want)
		}
	}
	// The next new rule continues the sequence; nothing is renumbered.
	if marks, err := b.Allocate(rulesPolicy("r3", "r4", "r5"), rulesPolicy("r3", "r4")); err != nil || marks["r5"] != 5 {
		t.Fatalf("allocation after load = %v, %v", marks, err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), data...)
	tampered[len(marksMagic)+3] ^= 0xff
	inconsistent := marksFile{Next: 2, Rules: []marksRule{{ID: "a", Mark: 1}, {ID: "b", Mark: 1}}}
	for name, content := range map[string][]byte{
		"truncated":    data[:len(data)-1],
		"tampered":     tampered,
		"foreign":      []byte("something else entirely, long enough to hold a checksum........."),
		"inconsistent": sealMarks(t, inconsistent),
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadAllocator(p); !errors.Is(err, ErrCorruptMarksFile) {
			t.Fatalf("%s file: %v", name, err)
		}
	}
}

func sealMarks(t *testing.T, f marksFile) []byte {
	t.Helper()
	dir := t.TempDir()
	a := newAllocator(uint32(MaxRules))
	a.next, a.gen = f.Next, f.Generation
	// Write through Save's format with deliberately duplicated values.
	for i, r := range f.Rules {
		a.byID[fmt.Sprint(r.ID, i)] = &slot{mark: r.Mark, gen: r.Generation}
	}
	path := filepath.Join(dir, "m")
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	return data
}

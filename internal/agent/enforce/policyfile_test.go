package enforce

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// TestPolicyFileRoundTrip checks that a saved policy loads back equal and
// canonical with its version, that the file is private, that a missing
// file loads nothing, that a truncated, tampered, or foreign file is
// refused as corrupt, and that a save replaces atomically (no temporary
// file is left behind).
func TestPolicyFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := PolicyFile{Path: filepath.Join(dir, "sub", PolicyFileName)}
	if p, err := f.Load(); p != nil || err != nil {
		t.Fatalf("Load of a missing file = %v, %v", p, err)
	}
	policy := &innerwallv1.WorkloadPolicy{Version: 42, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, InboundRules: []*innerwallv1.ResolvedRule{
		{RuleId: "b/tcp", Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.0.0.2", "10.0.0.1/32"}, Ports: []*innerwallv1.PortRange{{Start: 80, End: 80}}},
		{RuleId: "a/udp", Protocol: innerwallv1.Protocol_PROTOCOL_UDP, PeerCidrs: []string{"fd00::/64"}},
	}}
	if err := f.Save(policy); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(f.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat = %v, %v", info, err)
	}
	got, err := f.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.GetVersion() != 42 || !rendered.Equal(got, policy) || got.GetInboundRules()[0].GetRuleId() != "a/udp" {
		t.Fatalf("loaded = %v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(f.Path))
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries after save, want 1", len(entries))
	}

	// Replace, then corrupt in three ways.
	policy.Version = 43
	if err := f.Save(policy); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Load(); got.GetVersion() != 43 {
		t.Fatalf("replaced version = %d", got.GetVersion())
	}
	data, _ := os.ReadFile(f.Path)
	for name, bad := range map[string][]byte{
		"truncated": data[:len(data)-5],
		"tampered":  append(append([]byte{}, data[:len(policyMagic)+1]...), append([]byte{data[len(policyMagic)+1] ^ 0xff}, data[len(policyMagic)+2:]...)...),
		"foreign":   []byte("not a policy file at all, but long enough to have a checksum trailer"),
		"empty":     {},
	} {
		if err := os.WriteFile(f.Path, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		p, err := f.Load()
		if !errors.Is(err, ErrCorruptPolicyFile) || p != nil {
			t.Fatalf("%s: Load = %v, %v; want ErrCorruptPolicyFile", name, p, err)
		}
	}
	if err := f.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := f.Remove(); err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if p, err := f.Load(); p != nil || err != nil {
		t.Fatalf("Load after remove = %v, %v", p, err)
	}
}

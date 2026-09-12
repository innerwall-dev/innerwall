package registry_test

import (
	"net/netip"
	"testing"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

func TestAddressesFromFacts(t *testing.T) {
	facts := &innerwallv1.HostFacts{Interfaces: []*innerwallv1.NetworkInterface{
		{Name: "lo", Addresses: []string{"127.0.0.1/8", "::1/128"}},
		{Name: "eth0", Addresses: []string{"10.0.4.17/24", "fe80::1/64", "fd00::17/64", "10.0.4.17/24", "garbage"}},
		{Name: "eth1", Addresses: []string{"192.168.1.5", "0.0.0.0/0", "224.0.0.1/32"}},
	}}
	got := registry.AddressesFromFacts(facts)
	want := []netip.Addr{netip.MustParseAddr("10.0.4.17"), netip.MustParseAddr("192.168.1.5"), netip.MustParseAddr("fd00::17")}
	if len(got) != len(want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("addresses = %v, want %v", got, want)
		}
	}
	if got := registry.AddressesFromFacts(nil); len(got) != 0 {
		t.Fatalf("nil facts gave %v", got)
	}
}

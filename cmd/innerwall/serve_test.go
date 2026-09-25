package main

import "testing"

// TestCheckAdvertisedAddress pins the one form the advertised gateway
// address takes: empty for none, otherwise a host:port the agent's
// --server accepts. Nothing is resolved or derived.
func TestCheckAdvertisedAddress(t *testing.T) {
	for _, ok := range []string{"", "gateway.lab.example:8443", "10.20.4.17:8443", "[fd00::1]:8443", "localhost:1"} {
		if err := checkAdvertisedAddress(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{":8443", "gateway.lab.example", "gateway.lab.example:", "gateway.lab.example:0", "gateway.lab.example:65536", "gateway.lab.example:https", "fd00::1:8443"} {
		if err := checkAdvertisedAddress(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

package main

import (
	"context"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/authn"
)

// TestCommandContextPlacesThePrincipal pins the command line's entry path:
// every command reaches the domain layer as the one operator, and serve,
// which answers requests rather than making them, places none.
func TestCommandContextPlacesThePrincipal(t *testing.T) {
	for _, cmd := range []string{"token", "operator", "service", "address-group", "ruleset", "workload", "policy", "flows", "dev", "migrate", "ca"} {
		p, ok := authn.FromContext(commandContext(context.Background(), cmd))
		if !ok || p.DisplayName != nil {
			t.Errorf("%s: principal %+v, %v; want the operator with no display name", cmd, p, ok)
		}
	}
	if p, ok := authn.FromContext(commandContext(context.Background(), "serve")); ok {
		t.Errorf("serve placed principal %+v", p)
	}
}

package authn_test

import (
	"context"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/authn"
)

// TestPrincipalTravelsInContext pins the one way a principal reaches the
// domain boundary: the value placed is the value read, and a context that
// was never given one, or was given nil, reports none.
func TestPrincipalTravelsInContext(t *testing.T) {
	name := "Avery"
	placed := &authn.Principal{DisplayName: &name}
	got, ok := authn.FromContext(authn.NewContext(context.Background(), placed))
	if !ok || got != placed {
		t.Fatalf("FromContext = %v, %v; want the placed principal", got, ok)
	}
	if p, ok := authn.FromContext(context.Background()); ok || p != nil {
		t.Fatalf("a bare context reported principal %v", p)
	}
	if p, ok := authn.FromContext(authn.NewContext(context.Background(), nil)); ok || p != nil {
		t.Fatalf("a nil principal reported as %v", p)
	}
}

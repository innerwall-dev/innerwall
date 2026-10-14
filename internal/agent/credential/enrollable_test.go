package credential_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// TestEnrollable is the rule enrollment applies to a state directory: an
// empty one takes an enrollment, one holding a valid credential refuses
// it (a live identity), and one holding an expired credential is
// overwritable, with the expired leaf returned so the caller can say what
// it replaced.
func TestEnrollable(t *testing.T) {
	empty := credential.Store{Dir: t.TempDir()}
	if leaf, err := empty.Enrollable(time.Now()); err != nil || leaf != nil {
		t.Fatalf("empty directory: %v, %v", leaf, err)
	}

	h, _, id, store := newHolder(t, time.Hour)
	notAfter := h.Leaf().NotAfter
	if _, err := store.Enrollable(notAfter.Add(-time.Minute)); !errors.Is(err, credential.ErrHoldsCredential) {
		t.Fatalf("valid credential: err = %v, want ErrHoldsCredential", err)
	}
	leaf, err := store.Enrollable(notAfter.Add(time.Minute))
	if err != nil || leaf == nil {
		t.Fatalf("expired credential: %v, %v", leaf, err)
	}
	if got, _ := identity.FromCertificate(leaf); got != id {
		t.Fatalf("expired leaf names %v, want %v", got, id)
	}

	// A key whose credential cannot be read cannot be shown to have
	// expired, and is kept.
	if err := os.Remove(filepath.Join(store.Dir, credential.CertFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enrollable(notAfter.Add(time.Minute)); !errors.Is(err, credential.ErrHoldsCredential) {
		t.Fatalf("key without a certificate: err = %v, want ErrHoldsCredential", err)
	}
}

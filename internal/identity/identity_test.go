package identity

import (
	"crypto/x509"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

func TestURIRoundTrip(t *testing.T) {
	for range 20 {
		id, err := NewWorkloadID()
		if err != nil {
			t.Fatal(err)
		}
		u := id.URI()
		if u.String() != "innerwall://workload/"+id.String() {
			t.Fatalf("unexpected uri %q", u)
		}
		back, err := ParseURI(u)
		if err != nil {
			t.Fatalf("parse %q: %v", u, err)
		}
		if back != id {
			t.Fatalf("round trip mismatch: %v != %v", back, id)
		}
		back, err = ParseURIString(u.String())
		if err != nil {
			t.Fatalf("parse string %q: %v", u, err)
		}
		if back != id {
			t.Fatalf("string round trip mismatch: %v != %v", back, id)
		}
	}
}

func TestNewWorkloadIDIsVersion7(t *testing.T) {
	id, err := NewWorkloadID()
	if err != nil {
		t.Fatal(err)
	}
	if v := id.UUID().Version(); v != 7 {
		t.Fatalf("uuid version = %d, want 7", v)
	}
}

func TestParseURIRejectsNonCanonicalForms(t *testing.T) {
	id := uuid.MustParse("0192f4a0-2d6e-7c1a-9b3e-5f1c2d3e4f50")
	bad := []string{
		"",
		"other://workload/" + id.String(),
		"innerwall://Workload/" + id.String(),
		"innerwall://workload/",
		"innerwall://workload",
		"innerwall://workload/" + id.String() + "/extra",
		"innerwall://workload/" + id.String() + "?x=1",
		"innerwall://workload/" + id.String() + "#frag",
		"innerwall://user@workload/" + id.String(),
		"innerwall://workload:443/" + id.String(),
		"innerwall://workload/{" + id.String() + "}",
		"innerwall://workload/urn:uuid:" + id.String(),
		"innerwall://workload/0192f4a02d6e7c1a9b3e5f1c2d3e4f50",
		"innerwall://workload/not-a-uuid",
		"innerwall://workload/00000000-0000-0000-0000-000000000000",
		"innerwall:workload/" + id.String(),
	}
	for _, s := range bad {
		if _, err := ParseURIString(s); err == nil {
			t.Errorf("ParseURIString(%q) accepted, want error", s)
		}
	}
}

func TestFromCertificate(t *testing.T) {
	id, _ := NewWorkloadID()
	other, _ := NewWorkloadID()

	t.Run("exactly one uri san", func(t *testing.T) {
		got, err := FromCertificate(&x509.Certificate{URIs: []*url.URL{id.URI()}})
		if err != nil || got != id {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("no uri san", func(t *testing.T) {
		_, err := FromCertificate(&x509.Certificate{DNSNames: []string{"host"}})
		if !errors.Is(err, ErrNoIdentity) {
			t.Fatalf("err = %v, want ErrNoIdentity", err)
		}
	})
	t.Run("nil certificate", func(t *testing.T) {
		if _, err := FromCertificate(nil); !errors.Is(err, ErrNoIdentity) {
			t.Fatalf("err = %v, want ErrNoIdentity", err)
		}
	})
	t.Run("two uri sans", func(t *testing.T) {
		_, err := FromCertificate(&x509.Certificate{URIs: []*url.URL{id.URI(), other.URI()}})
		if !errors.Is(err, ErrAmbiguousIdentity) {
			t.Fatalf("err = %v, want ErrAmbiguousIdentity", err)
		}
	})
	t.Run("foreign uri san", func(t *testing.T) {
		u, _ := url.Parse("https://example.invalid/x")
		if _, err := FromCertificate(&x509.Certificate{URIs: []*url.URL{u}}); err == nil {
			t.Fatal("foreign uri accepted")
		}
	})
}

package credential

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/ca"
)

func TestStoreModesAndRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := Store{Dir: dir}
	if _, _, err := s.Load(); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("Load before enroll err = %v", err)
	}
	if _, err := s.LoadKey(); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("LoadKey before enroll err = %v", err)
	}
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := NewCSR(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.PublicKeyFromCSR(csr); err != nil {
		t.Fatalf("csr not accepted by the signing boundary: %v", err)
	}
	if err := s.Save(key, []byte("cert"), []byte("bundle")); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dir)
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", st.Mode().Perm())
	}
	for _, f := range []string{KeyFile, CertFile, BundleFile} {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", f, st.Mode().Perm())
		}
	}
	back, err := s.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(key) {
		t.Fatal("key round trip mismatch")
	}
	if err := s.ReplaceCertificate([]byte("cert2"), nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, CertFile)) //nolint:gosec // test temp dir
	if string(got) != "cert2" {
		t.Fatalf("cert = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

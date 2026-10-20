package store

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUnionFSMergesSourcesByName(t *testing.T) {
	a := fstest.MapFS{"00001_a.sql": {Data: []byte("a")}, "00003_c.sql": {Data: []byte("c")}}
	b := fstest.MapFS{"00002_b.sql": {Data: []byte("b")}, "sub/00009_x.sql": {Data: []byte("x")}}
	u, err := unionFS([]fs.FS{a, b})
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.Glob(u, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"00001_a.sql", "00002_b.sql", "00003_c.sql"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Glob = %v, want %v", got, want)
	}
	for name, want := range map[string]string{"00001_a.sql": "a", "00002_b.sql": "b", "00003_c.sql": "c"} {
		data, err := fs.ReadFile(u, name)
		if err != nil || string(data) != want {
			t.Fatalf("ReadFile(%s) = %q, %v; want %q", name, data, err, want)
		}
	}
	if _, err := u.Open("sub/00009_x.sql"); err == nil {
		t.Fatal("a file below a source's root was served")
	}
}

func TestUnionFSRefusesANameInTwoSources(t *testing.T) {
	a := fstest.MapFS{"00001_a.sql": {Data: []byte("a")}}
	b := fstest.MapFS{"00001_a.sql": {Data: []byte("other")}}
	if _, err := unionFS([]fs.FS{a, b}); err == nil || !strings.Contains(err.Error(), "00001_a.sql") {
		t.Fatalf("err = %v, want the duplicate named", err)
	}
	if _, err := unionFS(nil); err == nil {
		t.Fatal("no sources accepted")
	}
}

func TestEmbeddedMigrationsAreOneSource(t *testing.T) {
	u, err := unionFS([]fs.FS{Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := fs.Glob(u, "*.sql")
	want, _ := fs.Glob(Migrations(), "*.sql")
	if len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("union of the embedded set = %v, want %v", got, want)
	}
}

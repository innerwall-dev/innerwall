package enforce

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// watchSyncs replaces the two syncs for one test, recording each in
// order, and fails the one named by failOn.
func watchSyncs(t *testing.T, failOn string) *[]string {
	t.Helper()
	var calls []string
	origFile, origDir := syncFile, syncDir
	t.Cleanup(func() { syncFile, syncDir = origFile, origDir })
	syncFile = func(f *os.File) error {
		calls = append(calls, "file:"+filepath.Dir(f.Name()))
		if failOn == "file" {
			return errors.New("disk said no")
		}
		return origFile(f)
	}
	syncDir = func(dir string) error {
		calls = append(calls, "dir:"+dir)
		if failOn == "dir" {
			return errors.New("disk said no")
		}
		return origDir(dir)
	}
	return &calls
}

// TestWriteDurableSyncsFileThenDirectory checks the durable path: the
// temporary file is synced before the rename and the directory holding
// it after, the result is private, and nothing temporary is left behind.
func TestWriteDurableSyncsFileThenDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "f")
	calls := watchSyncs(t, "")
	if err := WriteDurable(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	want := []string{"file:" + filepath.Dir(path), "dir:" + filepath.Dir(path)}
	if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
		t.Fatalf("syncs = %v, want %v", *calls, want)
	}
	got, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil || string(got) != "new" {
		t.Fatalf("content = %q, %v", got, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat = %v, %v", info, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries; a temporary file was left behind", len(entries))
	}
}

// TestWriteDurableFailures checks both sync failures. A failed file sync
// is before the rename: the write fails, the old content stands, and the
// temporary file is removed. A failed directory sync is after it: the
// write fails, because the new content's durability is not established,
// and the store treats that as a failed persist.
func TestWriteDurableFailures(t *testing.T) {
	for _, failOn := range []string{"file", "dir"} {
		t.Run(failOn, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			watchSyncs(t, failOn)
			if err := WriteDurable(path, []byte("new")); err == nil {
				t.Fatal("WriteDurable succeeded with a failing sync")
			}
			got, _ := os.ReadFile(path) //nolint:gosec // test fixture
			if failOn == "file" && string(got) != "old" {
				t.Fatalf("content after a failed file sync = %q, want the old content", got)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatalf("directory holds %d entries; a temporary file was left behind", len(entries))
			}
		})
	}
}

// TestPolicyFileSaveIsDurable checks that the policy file goes through
// the durable path, and that a save whose directory sync fails is
// reported failed.
func TestPolicyFileSaveIsDurable(t *testing.T) {
	dir := t.TempDir()
	calls := watchSyncs(t, "dir")
	f := PolicyFile{Path: filepath.Join(dir, PolicyFileName)}
	if err := f.Save(nil); err == nil {
		t.Fatal("Save succeeded with a failing directory sync")
	}
	if len(*calls) != 2 || (*calls)[1] != "dir:"+dir {
		t.Fatalf("syncs = %v", *calls)
	}
}

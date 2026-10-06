package enforce

import (
	"fmt"
	"os"
	"path/filepath"
)

// The two syncs a durable write makes, as variables so a test can watch
// them happen in order and make either one fail.
var (
	syncFile = func(f *os.File) error { return f.Sync() }
	syncDir  = func(dir string) error {
		d, err := os.Open(dir) //nolint:gosec // the agent's own state directory
		if err != nil {
			return err
		}
		if err := d.Sync(); err != nil {
			_ = d.Close()
			return err
		}
		return d.Close()
	}
)

// WriteDurable replaces the file at path with data, mode 0600, so that a
// reader sees the old content or the new and never a mixture, and so that
// once it returns nil the new content survives a crash or a power loss: a
// temporary file beside path is written and synced, renamed into place,
// and then the directory holding it is synced, because the rename is an
// update of the directory and is not durable until the directory is. The
// directory is created, mode 0700, when missing.
//
// An error before the rename leaves the old file exactly as it was. An
// error from the directory sync comes after the rename: the new content
// is in place but its durability is not established, and the caller
// treats the write as failed.
func WriteDurable(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("enforce: creating state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("enforce: creating temporary file: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(what string, err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("enforce: %s %s: %w", what, filepath.Base(path), err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail("setting the mode of", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("writing", err)
	}
	if err := syncFile(tmp); err != nil {
		return fail("syncing", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("enforce: closing %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("enforce: installing %s: %w", filepath.Base(path), err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("enforce: syncing the state directory after installing %s: %w", filepath.Base(path), err)
	}
	return nil
}

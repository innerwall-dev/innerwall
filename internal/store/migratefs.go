package store

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
)

// union is a read-only file system over the root directories of several
// migration sources, each file name resolving to the one source that
// holds it. It serves exactly what goose reads: the root listing and the
// files in it.
type union struct {
	owner   map[string]fs.FS
	entries []fs.DirEntry
}

// unionFS builds the union of sources' root directories, refusing a file
// name that two sources both hold.
func unionFS(sources []fs.FS) (fs.FS, error) {
	if len(sources) == 0 {
		return nil, errors.New("store: no migration sources")
	}
	u := &union{owner: make(map[string]fs.FS)}
	for i, src := range sources {
		if src == nil {
			return nil, fmt.Errorf("store: migration source %d is nil", i)
		}
		entries, err := fs.ReadDir(src, ".")
		if err != nil {
			return nil, fmt.Errorf("store: reading migration source %d: %w", i, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if _, dup := u.owner[e.Name()]; dup {
				return nil, fmt.Errorf("store: migration %s is in more than one source", e.Name())
			}
			u.owner[e.Name()] = src
			u.entries = append(u.entries, e)
		}
	}
	sort.Slice(u.entries, func(i, j int) bool { return u.entries[i].Name() < u.entries[j].Name() })
	return u, nil
}

// Open opens a file at the root from the source that holds it. The root
// itself is read through ReadDir.
func (u *union) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	src, ok := u.owner[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return src.Open(name)
}

// ReadDir lists the root: every source's files, sorted by name.
func (u *union) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return append([]fs.DirEntry(nil), u.entries...), nil
}

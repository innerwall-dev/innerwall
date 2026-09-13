package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// consoleEntry is the console's entry point. Every path that is not a
// file in the tree is answered with it, and the console's router takes
// the path from there (ADR-0008).
const consoleEntry = "index.html"

// consoleAssetDir is where the build writes content-addressed files: the
// name of every file under it carries a hash of its content, so a file at
// a given path never changes and a browser may keep it for as long as it
// likes. Everything outside it is served with revalidation.
const consoleAssetDir = "assets/"

// consoleMountPoint is where the embedded console is served (ADR-0008,
// ADR-0021). A build that carries no console, and a build whose tree holds
// no entry point because `make console` has not run, answer every path
// outside the API prefix with a marked problem from here.
func consoleMountPoint(w http.ResponseWriter, _ *http.Request) {
	p := problemNotFound
	p.Detail = "the operator console is not served by this build; the API is under " + APIPrefix
	writeProblem(w, p)
}

// ConsoleHandler serves the console tree at the mount point. A nil tree,
// or one without an entry point, is the mount point answering not
// found. Otherwise a file at the request path is served as itself;
// content-addressed files under the asset directory are marked
// immutable, everything else must be revalidated; and any other path is
// the entry point, so a deep link into the console loads the console.
// Only reads are served: the surface's writes are all under the API
// prefix.
func ConsoleHandler(tree fs.FS) http.Handler {
	if tree == nil {
		return http.HandlerFunc(consoleMountPoint)
	}
	if _, err := fs.Stat(tree, consoleEntry); err != nil {
		return http.HandlerFunc(consoleMountPoint)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeProblem(w, Problem{Type: ProblemMethodNotAllowed, Title: http.StatusText(http.StatusMethodNotAllowed), Status: http.StatusMethodNotAllowed})
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "." {
			name = consoleEntry
		}
		data, err := fs.ReadFile(tree, name)
		switch {
		case err == nil && strings.HasPrefix(name, consoleAssetDir):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case err == nil:
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("ETag", etag(data))
		case strings.HasPrefix(name, consoleAssetDir):
			// A content-addressed file that is not in the tree belongs to
			// another build of the console; the entry point would be the
			// wrong answer, since a browser asked for a script.
			writeProblem(w, problemNotFound)
			return
		default:
			name = consoleEntry
			data, err = fs.ReadFile(tree, name)
			if err != nil {
				writeProblem(w, problemInternal)
				return
			}
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("ETag", etag(data))
		}
		if errors.Is(err, fs.ErrNotExist) {
			writeProblem(w, problemNotFound)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// The tree is embedded, so there is no modification time worth
		// telling a browser; the entity tag and the immutable marking
		// carry the caching story.
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}

// etag is a strong validator over the content itself, so a revalidation
// of the entry point costs a hash and no bytes when it has not changed.
func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

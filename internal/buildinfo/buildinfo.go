// Package buildinfo names the build a binary came from. Both binaries
// report the same string everywhere a version appears: the version
// subcommand, the startup log line, and the agent's reported version on the
// sync stream, which the console shows among a workload's host facts.
//
// The string is semantic-version shaped, with the commit as build metadata:
//
//	v0.1.0+6007581f652b        a build of a tagged commit
//	dev+f347cd966f58           a from-source build of an untagged commit
//	dev+f347cd966f58.dirty     the same, with uncommitted changes
//	dev+unknown                a build that carries no commit at all
//
// The commit comes from what the Go toolchain embeds on every build made in
// a git checkout (vcs.revision), so a plain `go build` needs no flags. A
// release build may set both values at link time instead:
//
//	-ldflags "-X github.com/innerwall-dev/innerwall/internal/buildinfo.version=v0.1.0
//	          -X github.com/innerwall-dev/innerwall/internal/buildinfo.commit=<sha>"
package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// Set at link time by a release build; empty otherwise.
var (
	version string
	commit  string
)

// shortLen is the commit prefix reported: the length the Go toolchain uses
// in its own pseudo-versions.
const shortLen = 12

// Version is this binary's version string.
func Version() string {
	bi, _ := debug.ReadBuildInfo()
	return format(version, commit, bi)
}

// pseudo matches the suffix of a Go pseudo-version, which the toolchain
// records for a build of an untagged commit. It names a version that was
// never released, so it is not reported as one.
var pseudo = regexp.MustCompile(`(^|[-.])(0\.)?\d{14}-[0-9a-f]{12}$`)

func format(ldVersion, ldCommit string, bi *debug.BuildInfo) string {
	var rev string
	var modified bool
	tag := ldVersion
	if bi != nil {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if tag == "" {
			tag = releaseTag(bi.Main.Version)
		}
	}
	if ldCommit != "" {
		rev, modified = ldCommit, false
	}
	if tag == "" {
		tag = "dev"
	}
	if rev == "" {
		if tag == "dev" {
			return "dev+unknown"
		}
		return tag
	}
	if len(rev) > shortLen {
		rev = rev[:shortLen]
	}
	v := tag + "+" + rev
	if modified {
		v += ".dirty"
	}
	return v
}

// releaseTag is the main module's version when it names a tag exactly:
// not "(devel)", not a pseudo-version. A dirty tree is reported through
// vcs.modified, so the toolchain's own +dirty suffix is dropped here.
func releaseTag(mv string) string {
	mv, _, _ = strings.Cut(mv, "+")
	if mv == "" || mv == "(devel)" || pseudo.MatchString(mv) {
		return ""
	}
	return mv
}

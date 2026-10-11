package buildinfo

import (
	"runtime/debug"
	"testing"
)

func info(mainVersion, rev, modified string) *debug.BuildInfo {
	bi := &debug.BuildInfo{Main: debug.Module{Path: "github.com/innerwall-dev/innerwall", Version: mainVersion}}
	if rev != "" {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: "vcs", Value: "git"}, debug.BuildSetting{Key: "vcs.revision", Value: rev})
	}
	if modified != "" {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: "vcs.modified", Value: modified})
	}
	return bi
}

const rev = "f347cd966f58e87842c8aa35cdccd44e19e29f6d"

func TestFormat(t *testing.T) {
	for _, tc := range []struct {
		name                string
		ldVersion, ldCommit string
		bi                  *debug.BuildInfo
		want                string
	}{
		{"untagged commit", "", "", info("v0.1.1-0.20261009230904-f347cd966f58", rev, "false"), "dev+f347cd966f58"},
		{"untagged commit, no tags at all", "", "", info("v0.0.0-20261009230904-f347cd966f58", rev, "false"), "dev+f347cd966f58"},
		{"untagged commit after a pre-release", "", "", info("v0.2.0-rc.1.0.20261009230904-f347cd966f58", rev, "false"), "dev+f347cd966f58"},
		{"dirty tree", "", "", info("v0.1.1-0.20261009230904-f347cd966f58+dirty", rev, "true"), "dev+f347cd966f58.dirty"},
		{"tagged commit", "", "", info("v0.1.0", rev, "false"), "v0.1.0+f347cd966f58"},
		{"tagged commit, dirty", "", "", info("v0.1.0+dirty", rev, "true"), "v0.1.0+f347cd966f58.dirty"},
		{"devel without vcs", "", "", info("(devel)", "", ""), "dev+unknown"},
		{"no build info", "", "", nil, "dev+unknown"},
		{"module download of a tag", "", "", info("v0.1.0", "", ""), "v0.1.0"},
		{"release link flags", "0.1.0", rev, info("(devel)", "", ""), "0.1.0+f347cd966f58"},
		{"release version over vcs", "0.1.0", "", info("v0.1.1-0.20261009230904-f347cd966f58", rev, "false"), "0.1.0+f347cd966f58"},
		{"link commit overrides a dirty stamp", "", rev, info("(devel)", "0000000000000000000000000000000000000000", "true"), "dev+f347cd966f58"},
		{"short commit kept whole", "", "f347cd9", nil, "dev+f347cd9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := format(tc.ldVersion, tc.ldCommit, tc.bi); got != tc.want {
				t.Errorf("format = %q, want %q", got, tc.want)
			}
		})
	}
}

// The test binary itself is never reported as bare "dev".
func TestVersionNeverBareDev(t *testing.T) {
	if v := Version(); v == "dev" || v == "" {
		t.Fatalf("Version() = %q", v)
	}
}

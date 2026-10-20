// Package cli holds what the two command lines, innerwall and
// innerwall-agent, share in reading their arguments.
package cli

import (
	"flag"
	"strings"
)

// Parse reads args into fs as fs.Parse does, except that flags may also
// follow the command's positional arguments: `flows list <workload-id>
// --since 1h` reads as `flows list --since 1h <workload-id>`.
//
// maxPositional is the most positional arguments the command takes. Flags
// are read around up to that many; a positional past them ends flag
// parsing as fs.Parse does, so it and everything after it stay positional
// for the command's own count to refuse. A command that takes none passes 0
// and parses exactly as fs.Parse would. `--` ends flag parsing wherever it
// appears, and a flag that takes a value takes the next argument whatever
// its shape, both as in fs.Parse.
//
// A command whose count refuses extra positionals therefore reads every
// invocation fs.Parse accepted the same way: under fs.Parse a flag after a
// positional was itself a positional, and over the count.
func Parse(fs *flag.FlagSet, args []string, maxPositional int) error {
	var flags, positional []string
	for len(args) > 0 {
		arg := args[0]
		if arg == "--" {
			positional = append(positional, args[1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			if len(positional) == maxPositional {
				positional = append(positional, args...)
				break
			}
			positional = append(positional, arg)
			args = args[1:]
			continue
		}
		flags = append(flags, arg)
		args = args[1:]
		if takesValue(fs, arg) {
			if len(args) == 0 {
				// fs.Parse reports the missing value.
				return fs.Parse(flags)
			}
			flags = append(flags, args[0])
			args = args[1:]
		}
	}
	return fs.Parse(append(append(flags, "--"), positional...))
}

// Leading reports how fs.Parse alone would read args: the names of the
// flags it would read, those before the first positional argument or
// `--`, and every argument from that positional on, `--` itself left out.
// It reads nothing into fs. A command whose earlier versions ignored some
// arguments uses it to refuse them rather than read them differently.
func Leading(fs *flag.FlagSet, args []string) (names, rest []string) {
	for len(args) > 0 {
		arg := args[0]
		if arg == "--" {
			return names, args[1:]
		}
		if len(arg) < 2 || arg[0] != '-' {
			return names, args
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(arg[1:], "-"), "=")
		names = append(names, name)
		args = args[1:]
		if takesValue(fs, arg) && len(args) > 0 {
			args = args[1:]
		}
	}
	return names, nil
}

// takesValue reports whether the flag arg names reads the next argument as
// its value: a defined, non-boolean flag written without "=".
func takesValue(fs *flag.FlagSet, arg string) bool {
	name := strings.TrimPrefix(arg[1:], "-")
	if strings.Contains(name, "=") {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		// fs.Parse refuses an undefined flag.
		return false
	}
	if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
		return false
	}
	return true
}

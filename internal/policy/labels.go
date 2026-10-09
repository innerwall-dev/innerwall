package policy

import (
	"errors"
	"fmt"
	"sort"
)

// The label grammar (ADR-0022). A label is how a workload is selected,
// and every surface writes it in one textual form, `key=value`, with
// requirements separated by whitespace and a key's alternative values by
// "|". A key or value that contained whitespace, "=", or "|" could not be
// written back in that form without ambiguity: what an operator typed as
// two labels could be stored as one, and nothing would show the
// difference. So every label is admitted against this grammar wherever it
// enters (a provisioning token's labels, a label edit, a selector, a read
// filter), and the checks below are the only definition of it.

// MaxLabelLength bounds a label key and a label value, in bytes.
const MaxLabelLength = 63

var (
	// ErrBadLabelKey is a key outside the grammar.
	ErrBadLabelKey = errors.New("policy: label key must be 1 to 63 letters, digits, '.', '_', '-', or '/', beginning and ending with a letter or digit")
	// ErrBadLabelValue is a value outside the grammar.
	ErrBadLabelValue = errors.New("policy: label value must be 1 to 63 letters, digits, '.', '_', or '-', beginning and ending with a letter or digit")
	// ErrDuplicateLabelKey is a key given twice in one label set.
	ErrDuplicateLabelKey = errors.New("policy: label key appears more than once")
)

// CheckLabelKey admits a label key: ErrEmptyLabelKey when it is empty,
// ErrBadLabelKey when it is outside the grammar, nil otherwise.
func CheckLabelKey(key string) error {
	if key == "" {
		return ErrEmptyLabelKey
	}
	if !inGrammar(key, true) {
		return ErrBadLabelKey
	}
	return nil
}

// CheckLabelValue admits a label value: ErrBadLabelValue when it is empty
// or outside the grammar, nil otherwise.
func CheckLabelValue(value string) error {
	if !inGrammar(value, false) {
		return ErrBadLabelValue
	}
	return nil
}

// inGrammar reports whether s is 1 to MaxLabelLength bytes of letters,
// digits, '.', '_', '-' (and '/' in a key), beginning and ending with a
// letter or digit.
func inGrammar(s string, key bool) bool {
	if len(s) == 0 || len(s) > MaxLabelLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if alnum {
			continue
		}
		if i == 0 || i == len(s)-1 {
			return false
		}
		switch {
		case c == '.' || c == '_' || c == '-':
		case c == '/' && key:
		default:
			return false
		}
	}
	return true
}

// LabelPair is one label as it enters the system.
type LabelPair struct {
	Key   string
	Value string
}

// ValidateLabelSet admits a label set (a token's, or a workload's whole
// set in a label edit): every key and value in the grammar, no key twice.
// Findings are located at labels[i] in the order given, with the
// offending text quoted so whitespace in it shows.
func ValidateLabelSet(labels []LabelPair) error {
	f := &Findings{}
	seen := map[string]int{}
	for i, l := range labels {
		path := fmt.Sprintf("labels[%d]", i)
		bad := false
		if err := CheckLabelKey(l.Key); err != nil {
			f.add(path, err, quoted(l.Key))
			bad = true
		}
		if err := CheckLabelValue(l.Value); err != nil {
			f.add(path, err, quoted(l.Key)+" = "+quoted(l.Value))
			bad = true
		}
		if bad {
			continue
		}
		if first, dup := seen[l.Key]; dup {
			f.add(path, ErrDuplicateLabelKey, fmt.Sprintf("%q also at labels[%d]", l.Key, first))
			continue
		}
		seen[l.Key] = i
	}
	return f.result()
}

// quoted shows a key or value as text with its quotes, so an empty one
// or one with whitespace in it is visible in a finding.
func quoted(s string) string { return fmt.Sprintf("%q", s) }

// sortedSelectorKeys is a selector's keys in order, so findings on it are
// reported the same way every time.
func sortedSelectorKeys(s Selector) []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

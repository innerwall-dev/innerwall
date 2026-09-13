package policy

import (
	"errors"
	"strconv"
	"time"
)

// A resource version is the opaque string a conditional write names: the
// version the caller last read, which the write applies against and
// refuses when it has moved. For the authored objects it is derived from
// the instant of the last write, which every authoring write advances, so
// two versions of one object are never equal and the store can check the
// condition inside the write itself. Callers compare versions and never
// interpret them; the transport carries them as entity tags.

// VersionOf returns the version of an object last written at t.
func VersionOf(t time.Time) string {
	return strconv.FormatInt(t.UTC().UnixNano(), 10)
}

// TimeOfVersion is the inverse of VersionOf. It reports false for a string
// that is not a version this package issued.
func TimeOfVersion(v string) (time.Time, bool) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, n).UTC(), true
}

// ErrVersionMismatch is returned by a conditional write whose expected
// version is not the object's current one. The error carrying it is a
// *VersionMismatchError naming the current version.
var ErrVersionMismatch = errors.New("policy: the object has changed since it was read")

// VersionMismatchError is a refused conditional write with the version the
// object holds now, so the caller can re-read and decide.
type VersionMismatchError struct {
	Current string
}

func (e *VersionMismatchError) Error() string {
	return ErrVersionMismatch.Error() + " (current version " + e.Current + ")"
}

func (e *VersionMismatchError) Unwrap() error { return ErrVersionMismatch }

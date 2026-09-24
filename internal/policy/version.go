package policy

import (
	"errors"
	"strconv"
)

// A resource version is the token a conditional write names: the version
// the caller last read, which the write applies against and refuses when
// it has moved. Every authored object carries a monotonic integer that
// starts at 1 and advances by one with each write of the object, inside
// the statement that writes it. The token is that integer's decimal form;
// it is compared byte-exact against the stored value's form and never
// parsed, so callers hold it as opaque and the transport carries it as an
// entity tag.

// FormatVersion returns the token for the stored version v.
func FormatVersion(v int64) string {
	return strconv.FormatInt(v, 10)
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

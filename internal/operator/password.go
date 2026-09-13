package operator

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is the shortest password SetPassword accepts. Length is
// the one property of a password that reliably matters; composition rules
// are not enforced.
const MinPasswordLength = 12

// The argon2id parameters a new hash is computed with. They are written
// into the stored string, so raising them later changes nothing about
// verification of existing hashes and needs no migration; a hash is simply
// recomputed the next time the password is set.
const (
	argonTime    = 2
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

var errHashMalformed = errors.New("operator: stored password hash is malformed")

// HashPassword computes an argon2id hash of password and returns it in the
// self-describing form
//
//	$argon2id$v=19$m=<KiB>,t=<iterations>,p=<lanes>$<salt>$<hash>
//
// with salt and hash in unpadded standard base64.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("operator: generating salt: %w", err)
	}
	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum)), nil
}

// VerifyPassword reports whether password matches the stored hash,
// recomputing with the parameters the hash carries. It returns
// ErrPasswordMismatch on a wrong password and a distinct error on a hash
// that cannot be parsed, because the two mean different things to an
// operator.
func VerifyPassword(encoded, password string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return errHashMalformed
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return errHashMalformed
	}
	var memory, iterations uint32
	var lanes uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &lanes); err != nil {
		return errHashMalformed
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return errHashMalformed
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return errHashMalformed
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, lanes, uint32(len(want))) //nolint:gosec // len(want) is a decoded digest length, far below the uint32 range
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

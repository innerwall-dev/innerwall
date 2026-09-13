package operator

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
)

// TokenPrefix marks an operator token in transit. It differs from the
// provisioning token's prefix so a secret scanner, and the surface, can tell
// the two apart without knowledge of the deployment that minted them.
const TokenPrefix = "iwo_"

// listPrefixChars is how many characters of the random body are kept
// beside the prefix for listing. Enough to tell tokens apart in a table,
// far too few to shorten a guess.
const listPrefixChars = 8

// secretEntropyBytes is the random payload of a session identifier and of a
// token: 256 bits.
const secretEntropyBytes = 32

func randomSecret() (string, error) {
	raw := make([]byte, secretEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("operator: generating secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func digest(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

// HashEqual compares two digests in constant time.
func HashEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// NewSessionID draws a fresh session identifier. The plaintext goes to the
// browser; the hash is what gets stored.
func NewSessionID() (plaintext string, hash []byte, err error) {
	plaintext, err = randomSecret()
	if err != nil {
		return "", nil, err
	}
	return plaintext, digest(plaintext), nil
}

// HashSessionID validates the shape of a session identifier and returns its
// SHA-256 digest, or ErrSessionMalformed.
func HashSessionID(plaintext string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil || len(raw) != secretEntropyBytes {
		return nil, ErrSessionMalformed
	}
	return digest(plaintext), nil
}

// NewToken draws a fresh operator token. The plaintext is returned once, for
// display to the operator; the hash is stored, and the prefix is what a
// listing shows.
func NewToken() (plaintext string, hash []byte, prefix string, err error) {
	body, err := randomSecret()
	if err != nil {
		return "", nil, "", err
	}
	plaintext = TokenPrefix + body
	return plaintext, digest(plaintext), plaintext[:len(TokenPrefix)+listPrefixChars], nil
}

// HashToken validates the shape of a plaintext token and returns its SHA-256
// digest. Shape validation happens before hashing so a malformed value is
// reported as malformed rather than as unknown.
func HashToken(plaintext string) ([]byte, error) {
	if !strings.HasPrefix(plaintext, TokenPrefix) {
		return nil, ErrTokenMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(plaintext, TokenPrefix))
	if err != nil || len(raw) != secretEntropyBytes {
		return nil, ErrTokenMalformed
	}
	return digest(plaintext), nil
}

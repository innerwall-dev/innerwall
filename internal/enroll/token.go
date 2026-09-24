package enroll

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TokenPrefix marks a provisioning token in transit. A fixed prefix on an
// otherwise random secret lets secret scanners recognize a leaked token
// without any knowledge of the deployment that minted it.
const TokenPrefix = "iw_"

// tokenEntropyBytes is the random payload of a token: 256 bits.
const tokenEntropyBytes = 32

// listPrefixChars is how many characters of the random body are kept
// beside the prefix as the listing hint: enough to tell tokens apart in a
// table, far too few to matter to anyone guessing the rest. It matches the
// operator token's (ADR-0021).
const listPrefixChars = 8

// DefaultTokenTTL is the lifetime of a token when the operator sets none.
const DefaultTokenTTL = 30 * 24 * time.Hour

// Errors a token check can produce. They are distinct so an operator sees
// exactly why an enrollment failed; none of them reveals whether a token
// with a different value would have succeeded.
var (
	ErrTokenMalformed = errors.New("enroll: provisioning token is malformed")
	ErrTokenUnknown   = errors.New("enroll: provisioning token is not recognized")
	ErrTokenExpired   = errors.New("enroll: provisioning token has expired")
	ErrTokenRevoked   = errors.New("enroll: provisioning token has been revoked")
)

// Label is one key/value pair assigned to a workload.
type Label struct {
	Key   string
	Value string
}

// Token is the stored form of a provisioning token. It never carries the
// plaintext: Hash is what is looked up, and Prefix, the listing hint, is
// what is listed. Prefix is nil for a token minted before hints were kept,
// since none can be recovered from a digest.
type Token struct {
	ID         uuid.UUID
	Hash       []byte
	Prefix     *string
	Name       string
	Labels     []Label
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	UseCount   int64
	LastUsedAt *time.Time
}

// NewToken draws a fresh token. The plaintext is returned once, for display
// to the operator; the hash is what gets stored.
func NewToken() (plaintext string, hash []byte, err error) {
	raw := make([]byte, tokenEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("enroll: generating token: %w", err)
	}
	plaintext = TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	hash, err = HashToken(plaintext)
	if err != nil {
		return "", nil, err
	}
	return plaintext, hash, nil
}

// ListingHint is the part of a plaintext token kept for listing: the fixed
// prefix and the first characters of the random body.
func ListingHint(plaintext string) string {
	n := len(TokenPrefix) + listPrefixChars
	if len(plaintext) < n {
		return plaintext
	}
	return plaintext[:n]
}

// HashToken validates the shape of a plaintext token and returns its SHA-256
// digest. Shape validation happens before hashing so a malformed value is
// reported as malformed rather than as unknown. The digest is what is looked
// up; a store never sees the plaintext.
func HashToken(plaintext string) ([]byte, error) {
	if !strings.HasPrefix(plaintext, TokenPrefix) {
		return nil, ErrTokenMalformed
	}
	body := strings.TrimPrefix(plaintext, TokenPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || len(raw) != tokenEntropyBytes {
		return nil, ErrTokenMalformed
	}
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:], nil
}

// HashEqual compares two token hashes in constant time.
func HashEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// Check reports whether the token may enroll a workload at instant now.
// Revocation is checked before expiry so that a revoked token reports as
// revoked even after it would have expired anyway; the operator's action is
// the more specific fact.
func (t *Token) Check(now time.Time) error {
	if t == nil {
		return ErrTokenUnknown
	}
	if t.RevokedAt != nil && !t.RevokedAt.After(now) {
		return ErrTokenRevoked
	}
	if !t.ExpiresAt.After(now) {
		return ErrTokenExpired
	}
	return nil
}

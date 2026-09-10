package enroll

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewTokenShapeAndHash(t *testing.T) {
	plain, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, TokenPrefix) {
		t.Fatalf("token %q lacks prefix", plain)
	}
	if len(plain) != len(TokenPrefix)+43 {
		t.Fatalf("token length = %d, want %d", len(plain), len(TokenPrefix)+43)
	}
	want := sha256.Sum256([]byte(plain))
	if !HashEqual(hash, want[:]) {
		t.Fatal("hash is not sha256 of plaintext")
	}
	again, err := HashToken(plain)
	if err != nil || !HashEqual(again, hash) {
		t.Fatalf("HashToken mismatch: %v", err)
	}
	other, _, _ := NewToken()
	if other == plain {
		t.Fatal("two tokens collided")
	}
}

func TestHashTokenRejectsMalformed(t *testing.T) {
	plain, _, _ := NewToken()
	for _, s := range []string{
		"",
		"iw_",
		strings.TrimPrefix(plain, TokenPrefix),
		"IW_" + strings.TrimPrefix(plain, TokenPrefix),
		plain + "x",
		plain[:len(plain)-1],
		"iw_" + strings.Repeat("A", 43) + "=",
		"iw_not base64!",
	} {
		if _, err := HashToken(s); !errors.Is(err, ErrTokenMalformed) {
			t.Errorf("HashToken(%q) err = %v, want ErrTokenMalformed", s, err)
		}
	}
}

func TestTokenCheckStates(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name string
		tok  *Token
		want error
	}{
		{"valid", &Token{ExpiresAt: future}, nil},
		{"expired", &Token{ExpiresAt: past}, ErrTokenExpired},
		{"expires exactly now", &Token{ExpiresAt: now}, ErrTokenExpired},
		{"revoked", &Token{ExpiresAt: future, RevokedAt: &past}, ErrTokenRevoked},
		{"revoked exactly now", &Token{ExpiresAt: future, RevokedAt: &now}, ErrTokenRevoked},
		{"revoked and expired reports revoked", &Token{ExpiresAt: past, RevokedAt: &past}, ErrTokenRevoked},
		{"unknown", nil, ErrTokenUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.tok.Check(now); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

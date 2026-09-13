package operator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/operator/operatortest"
)

func ptr(s string) *string { return &s }

func newService(t *testing.T) (*operator.Service, *operatortest.MemStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	st := operatortest.NewMemStore()
	svc := &operator.Service{Store: st, Now: func() time.Time { return now }}
	return svc, st, &now
}

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := operator.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=2,p=2$") {
		t.Fatalf("hash is not self-describing: %s", hash)
	}
	if err := operator.VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := operator.VerifyPassword(hash, "correct horse battery stapl"); !errors.Is(err, operator.ErrPasswordMismatch) {
		t.Fatalf("wrong password: got %v, want ErrPasswordMismatch", err)
	}
	// Two hashes of one password differ by salt and both verify.
	again, err := operator.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if again == hash {
		t.Fatal("two hashes share a salt")
	}
	if err := operator.VerifyPassword(again, "correct horse battery staple"); err != nil {
		t.Fatalf("verify second hash: %v", err)
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$YQ$YQ", "$argon2id$v=18$m=1,t=1,p=1$YQ$YQ", "$argon2id$v=19$m=1,t=1,p=1$!!$YQ"} {
		if err := operator.VerifyPassword(bad, "x"); err == nil || errors.Is(err, operator.ErrPasswordMismatch) {
			t.Errorf("malformed hash %q: got %v, want a malformed-hash error", bad, err)
		}
	}
}

func TestSetAndVerifyPassword(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if err := svc.VerifyPassword(ctx, "anything at all"); !errors.Is(err, operator.ErrNoOperator) {
		t.Fatalf("before set-password: got %v, want ErrNoOperator", err)
	}
	if err := svc.SetPassword(ctx, "short", nil); !errors.Is(err, operator.ErrPasswordTooShort) {
		t.Fatalf("short password: got %v, want ErrPasswordTooShort", err)
	}
	if err := svc.SetPassword(ctx, "a perfectly fine password", ptr("Ada")); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPassword(ctx, "a perfectly fine password"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := svc.VerifyPassword(ctx, "a perfectly wrong password"); !errors.Is(err, operator.ErrPasswordMismatch) {
		t.Fatalf("wrong password: got %v", err)
	}
	// Setting again replaces the password and the display name.
	if err := svc.SetPassword(ctx, "a different fine password", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPassword(ctx, "a perfectly fine password"); !errors.Is(err, operator.ErrPasswordMismatch) {
		t.Fatalf("old password still verifies: %v", err)
	}
	if err := svc.VerifyPassword(ctx, "a different fine password"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	op, err := svc.Store.GetOperator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if op.DisplayName != nil {
		t.Fatalf("display name not cleared: %q", *op.DisplayName)
	}
}

func TestSessionLifecycle(t *testing.T) {
	svc, st, now := newService(t)
	ctx := context.Background()
	if err := svc.SetPassword(ctx, "a perfectly fine password", ptr("Ada")); err != nil {
		t.Fatal(err)
	}

	id, sess, err := svc.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sess.ExpiresAt, now.Add(operator.DefaultSessionTTL); !got.Equal(want) {
		t.Fatalf("expiry %v, want %v", got, want)
	}
	if strings.Contains(id, "=") || len(id) != 43 {
		t.Fatalf("session id %q is not 32 bytes of unpadded base64", id)
	}
	// The store holds the digest, never the identifier.
	if _, err := st.GetSession(ctx, []byte(id)); !errors.Is(err, operator.ErrSessionUnknown) {
		t.Fatalf("lookup by plaintext found a session: %v", err)
	}

	p, err := svc.ValidateSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayName == nil || *p.DisplayName != "Ada" {
		t.Fatalf("principal %+v", p)
	}
	for _, bad := range []string{"", "nope", strings.Repeat("A", 43), id[:42]} {
		_, err := svc.ValidateSession(ctx, bad)
		if bad == strings.Repeat("A", 43) {
			// Well-formed but never issued.
			if !errors.Is(err, operator.ErrSessionUnknown) {
				t.Errorf("unknown session %q: got %v", bad, err)
			}
			continue
		}
		if !errors.Is(err, operator.ErrSessionMalformed) {
			t.Errorf("malformed session %q: got %v", bad, err)
		}
	}

	// Fixed lifetime: one second short is live, the instant itself is not.
	*now = sess.ExpiresAt.Add(-time.Second)
	if _, err := svc.ValidateSession(ctx, id); err != nil {
		t.Fatalf("just before expiry: %v", err)
	}
	*now = sess.ExpiresAt
	if _, err := svc.ValidateSession(ctx, id); !errors.Is(err, operator.ErrSessionExpired) {
		t.Fatalf("at expiry: got %v, want ErrSessionExpired", err)
	}
	// Retention removes it; afterwards it is unknown rather than expired.
	n, err := svc.PruneSessions(ctx, *now)
	if err != nil || n != 1 {
		t.Fatalf("prune: n=%d err=%v", n, err)
	}
	if _, err := svc.ValidateSession(ctx, id); !errors.Is(err, operator.ErrSessionUnknown) {
		t.Fatalf("after prune: got %v, want ErrSessionUnknown", err)
	}

	// Delete revokes a live session; deleting again or deleting garbage is fine.
	*now = now.Add(-time.Hour)
	id2, _, err := svc.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSession(ctx, id2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateSession(ctx, id2); !errors.Is(err, operator.ErrSessionUnknown) {
		t.Fatalf("after delete: got %v", err)
	}
	if err := svc.DeleteSession(ctx, id2); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSession(ctx, "garbage"); err != nil {
		t.Fatal(err)
	}
	if st.SessionCount() != 0 {
		t.Fatalf("%d sessions remain", st.SessionCount())
	}
}

func TestTokenLifecycle(t *testing.T) {
	svc, st, now := newService(t)
	ctx := context.Background()

	// A token minted before any password exists resolves to a principal
	// without a display name.
	plain, tok, err := svc.MintToken(ctx, "ci", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, operator.TokenPrefix) || len(plain) != len(operator.TokenPrefix)+43 {
		t.Fatalf("token %q has the wrong shape", plain)
	}
	if tok.Prefix != plain[:12] {
		t.Fatalf("listing prefix %q, want %q", tok.Prefix, plain[:12])
	}
	if tok.ExpiresAt != nil {
		t.Fatalf("no ttl requested but expiry set: %v", tok.ExpiresAt)
	}
	if _, err := st.FindOperatorTokenByHash(ctx, []byte(plain)); !errors.Is(err, operator.ErrTokenUnknown) {
		t.Fatalf("lookup by plaintext found a token: %v", err)
	}
	p, err := svc.ValidateToken(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayName != nil {
		t.Fatalf("principal %+v", p)
	}
	listed, err := svc.ListTokens(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %v %v", listed, err)
	}
	if listed[0].LastUsedAt == nil || !listed[0].LastUsedAt.Equal(*now) {
		t.Fatalf("use not recorded: %+v", listed[0])
	}

	// Once the password exists the same token carries the display name.
	if err := svc.SetPassword(ctx, "a perfectly fine password", ptr("Ada")); err != nil {
		t.Fatal(err)
	}
	p, err = svc.ValidateToken(ctx, plain)
	if err != nil || p.DisplayName == nil || *p.DisplayName != "Ada" {
		t.Fatalf("principal %+v, err %v", p, err)
	}

	for _, bad := range []string{"", "iw_" + plain[4:], plain[:len(plain)-1], operator.TokenPrefix + "not base64!"} {
		if _, err := svc.ValidateToken(ctx, bad); !errors.Is(err, operator.ErrTokenMalformed) {
			t.Errorf("malformed token %q: got %v", bad, err)
		}
	}
	if _, err := svc.ValidateToken(ctx, operator.TokenPrefix+strings.Repeat("A", 43)); !errors.Is(err, operator.ErrTokenUnknown) {
		t.Fatalf("unknown token: got %v", err)
	}

	// Expiry.
	plainTTL, tokTTL, err := svc.MintToken(ctx, "short-lived", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if tokTTL.ExpiresAt == nil || !tokTTL.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expiry %v", tokTTL.ExpiresAt)
	}
	*now = now.Add(time.Hour)
	if _, err := svc.ValidateToken(ctx, plainTTL); !errors.Is(err, operator.ErrTokenExpired) {
		t.Fatalf("expired token: got %v", err)
	}

	// Revocation, and revocation outranks expiry.
	if err := svc.RevokeToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, plain); !errors.Is(err, operator.ErrTokenRevoked) {
		t.Fatalf("revoked token: got %v", err)
	}
	if err := svc.RevokeToken(ctx, tok.ID); !errors.Is(err, operator.ErrTokenRevoked) {
		t.Fatalf("second revoke: got %v", err)
	}
	if err := svc.RevokeToken(ctx, uuid.New()); !errors.Is(err, operator.ErrTokenUnknown) {
		t.Fatalf("revoke unknown: got %v", err)
	}
	if err := svc.RevokeToken(ctx, tokTTL.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, plainTTL); !errors.Is(err, operator.ErrTokenRevoked) {
		t.Fatalf("revoked and expired token: got %v, want revoked", err)
	}
}

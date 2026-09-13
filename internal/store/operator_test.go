package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

func TestOperatorSingleRow(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := st.GetOperator(ctx); !errors.Is(err, operator.ErrNoOperator) {
		t.Fatalf("empty: got %v, want ErrNoOperator", err)
	}
	name := "Ada"
	if err := st.SetOperator(ctx, operator.Operator{PasswordHash: "h1", DisplayName: &name, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	op, err := st.GetOperator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if op.PasswordHash != "h1" || op.DisplayName == nil || *op.DisplayName != "Ada" || !op.CreatedAt.Equal(now) {
		t.Fatalf("operator %+v", op)
	}
	// A second set replaces in place: still one row, created_at kept.
	later := now.Add(time.Minute)
	if err := st.SetOperator(ctx, operator.Operator{PasswordHash: "h2", CreatedAt: later, UpdatedAt: later}); err != nil {
		t.Fatal(err)
	}
	op, err = st.GetOperator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if op.PasswordHash != "h2" || op.DisplayName != nil || !op.CreatedAt.Equal(now) || !op.UpdatedAt.Equal(later) {
		t.Fatalf("operator after replace %+v", op)
	}
}

func TestOperatorSessions(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	live := operator.Session{IDHash: []byte("live-hash-000000000000000000000"), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	dead := operator.Session{IDHash: []byte("dead-hash-000000000000000000000"), CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}
	for _, s := range []operator.Session{live, dead} {
		if err := st.CreateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetSession(ctx, live.IDHash)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(live.ExpiresAt) {
		t.Fatalf("session %+v", got)
	}
	if _, err := st.GetSession(ctx, []byte("missing")); !errors.Is(err, operator.ErrSessionUnknown) {
		t.Fatalf("unknown: got %v", err)
	}
	// The store returns an expired row; judging expiry is the domain's job.
	if _, err := st.GetSession(ctx, dead.IDHash); err != nil {
		t.Fatalf("expired row: %v", err)
	}
	n, err := st.DeleteExpiredSessions(ctx, now)
	if err != nil || n != 1 {
		t.Fatalf("prune: n=%d err=%v", n, err)
	}
	if _, err := st.GetSession(ctx, dead.IDHash); !errors.Is(err, operator.ErrSessionUnknown) {
		t.Fatalf("pruned row still present: %v", err)
	}
	if err := st.DeleteSession(ctx, live.IDHash); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, live.IDHash); !errors.Is(err, operator.ErrSessionUnknown) {
		t.Fatalf("deleted row still present: %v", err)
	}
	if err := st.DeleteSession(ctx, live.IDHash); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestOperatorTokens(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	plain, hash, prefix, err := operator.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	exp := now.Add(24 * time.Hour)
	tok := operator.Token{ID: uuid.New(), Hash: hash, Prefix: prefix, Name: "ci", CreatedAt: now, ExpiresAt: &exp}
	if err := st.CreateOperatorToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	// Only the digest is stored: the plaintext finds nothing.
	if _, err := st.FindOperatorTokenByHash(ctx, []byte(plain)); !errors.Is(err, operator.ErrTokenUnknown) {
		t.Fatalf("lookup by plaintext: got %v", err)
	}
	got, err := st.FindOperatorTokenByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != tok.ID || got.Prefix != prefix || got.Name != "ci" || got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) || got.LastUsedAt != nil {
		t.Fatalf("token %+v", got)
	}
	if err := st.RecordOperatorTokenUse(ctx, tok.ID, now); err != nil {
		t.Fatal(err)
	}
	// A second token with no expiry, minted later, lists first.
	_, hash2, prefix2, err := operator.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	tok2 := operator.Token{ID: uuid.New(), Hash: hash2, Prefix: prefix2, CreatedAt: now.Add(time.Second)}
	if err := st.CreateOperatorToken(ctx, tok2); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListOperatorTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != tok2.ID || list[1].ID != tok.ID {
		t.Fatalf("list order: %+v", list)
	}
	if list[0].ExpiresAt != nil || list[1].LastUsedAt == nil || !list[1].LastUsedAt.Equal(now) {
		t.Fatalf("list detail: %+v", list)
	}

	if err := st.RevokeOperatorToken(ctx, tok.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeOperatorToken(ctx, tok.ID, now); !errors.Is(err, operator.ErrTokenRevoked) {
		t.Fatalf("second revoke: got %v", err)
	}
	if err := st.RevokeOperatorToken(ctx, uuid.New(), now); !errors.Is(err, operator.ErrTokenUnknown) {
		t.Fatalf("revoke unknown: got %v", err)
	}
	got, err = st.FindOperatorTokenByHash(ctx, hash)
	if err != nil || got.RevokedAt == nil || !got.RevokedAt.Equal(now) {
		t.Fatalf("revoked token %+v err %v", got, err)
	}
}

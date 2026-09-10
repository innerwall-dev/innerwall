package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

func TestTokensAndWorkloads(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)

	plain, hash, err := enroll.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Microsecond) // Postgres precision
	tok := enroll.Token{
		ID:        uuid.New(),
		Hash:      hash,
		Name:      "prod-db",
		Labels:    []enroll.Label{{Key: "env", Value: "prod"}, {Key: "role", Value: "db"}},
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := s.CreateToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateToken(ctx, tok); err == nil {
		t.Fatal("duplicate hash accepted")
	}

	got, err := s.FindTokenByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != tok.ID || got.Name != "prod-db" || !got.ExpiresAt.Equal(tok.ExpiresAt) || got.RevokedAt != nil {
		t.Fatalf("token = %+v", got)
	}
	if len(got.Labels) != 2 || got.Labels[0] != tok.Labels[0] || got.Labels[1] != tok.Labels[1] {
		t.Fatalf("labels = %v", got.Labels)
	}
	// The plaintext is never a lookup key.
	if _, err := s.FindTokenByHash(ctx, []byte(plain)); !errors.Is(err, enroll.ErrTokenUnknown) {
		t.Fatalf("plaintext lookup err = %v", err)
	}

	id, _ := identity.NewWorkloadID()
	w := enroll.Workload{
		ID:                  id,
		TokenID:             tok.ID,
		Hostname:            "db-1",
		Labels:              got.Labels,
		EnrolledAt:          now,
		CredentialSerial:    "abc",
		CredentialExpiresAt: now.Add(24 * time.Hour),
	}
	if err := s.CreateWorkload(ctx, w, now); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetWorkload(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Hostname != "db-1" || stored.CredentialSerial != "abc" || stored.TokenID != tok.ID || len(stored.Labels) != 2 {
		t.Fatalf("workload = %+v", stored)
	}
	used, _ := s.FindTokenByHash(ctx, hash)
	if used.UseCount != 1 || used.LastUsedAt == nil {
		t.Fatalf("token use not recorded: %+v", used)
	}

	if err := s.RecordRenewal(ctx, id, "def", now.Add(48*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	stored, _ = s.GetWorkload(ctx, id)
	if stored.CredentialSerial != "def" || stored.LastRenewedAt == nil {
		t.Fatalf("renewal not recorded: %+v", stored)
	}
	stranger, _ := identity.NewWorkloadID()
	if err := s.RecordRenewal(ctx, stranger, "x", now, now); !errors.Is(err, enroll.ErrWorkloadUnknown) {
		t.Fatalf("renewal of unknown: err = %v", err)
	}
	if _, err := s.GetWorkload(ctx, stranger); !errors.Is(err, enroll.ErrWorkloadUnknown) {
		t.Fatalf("get unknown: err = %v", err)
	}

	if err := s.RevokeToken(ctx, tok.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeToken(ctx, tok.ID, now); !errors.Is(err, enroll.ErrTokenRevoked) {
		t.Fatalf("second revoke err = %v", err)
	}
	if err := s.RevokeToken(ctx, uuid.New(), now); !errors.Is(err, enroll.ErrTokenUnknown) {
		t.Fatalf("revoke unknown err = %v", err)
	}
	revoked, _ := s.FindTokenByHash(ctx, hash)
	if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(now) {
		t.Fatalf("revocation not stored: %+v", revoked)
	}

	list, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != tok.ID || len(list[0].Labels) != 2 {
		t.Fatalf("list = %+v", list)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	url := os.Getenv(storetest.EnvDatabaseURL)
	if url == "" {
		t.Skipf("%s not set", storetest.EnvDatabaseURL)
	}
	if err := store.Migrate(context.Background(), url); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background(), url); err != nil {
		t.Fatal(err)
	}
}

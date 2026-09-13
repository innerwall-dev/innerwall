package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the Postgres implementation of the domain persistence
// interfaces. It is safe for concurrent use.
type Store struct {
	pool  *pgxpool.Pool
	q     *db.Queries
	flows *flowstore.Postgres
}

var _ enroll.Store = (*Store)(nil)

// Open connects to Postgres and verifies the connection.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: opening pool: %w", err)
	}
	s := &Store{pool: pool, q: db.New(pool), flows: flowstore.NewPostgres(pool)}
	if _, err := s.q.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return s, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// Flows returns the FlowStore over the same database. It is the only path
// to flow data (ADR-0009).
func (s *Store) Flows() *flowstore.Postgres { return s.flows }

// Migrate applies every pending migration in internal/store/migrations. It is
// idempotent: a fully migrated database is a no-op. Migrations run through
// the same goose engine the CLI uses, against the same embedded files that
// sqlc compiled the queries against. A session-level advisory lock
// serializes concurrent callers (two replicas starting with --migrate, or
// test packages sharing one database), so exactly one applies each file.
func Migrate(ctx context.Context, databaseURL string) error {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("store: parsing database url: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg)
	defer func() { _ = sqlDB.Close() }()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("store: preparing migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrationsFS(), goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("store: preparing migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("store: applying migrations: %w", err)
	}
	return nil
}

// --- provisioning tokens ---------------------------------------------------

// CreateToken implements enroll.Store.
func (s *Store) CreateToken(ctx context.Context, t enroll.Token) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if _, err := q.CreateProvisioningToken(ctx, db.CreateProvisioningTokenParams{
			ID:        t.ID,
			TokenHash: t.Hash,
			Name:      t.Name,
			ExpiresAt: t.ExpiresAt,
		}); err != nil {
			return fmt.Errorf("store: creating token: %w", err)
		}
		for _, l := range t.Labels {
			if err := q.AddProvisioningTokenLabel(ctx, db.AddProvisioningTokenLabelParams{TokenID: t.ID, Key: l.Key, Value: l.Value}); err != nil {
				return fmt.Errorf("store: adding token label: %w", err)
			}
		}
		return nil
	})
}

// FindTokenByHash implements enroll.Store.
func (s *Store) FindTokenByHash(ctx context.Context, hash []byte) (*enroll.Token, error) {
	row, err := s.q.GetProvisioningTokenByHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, enroll.ErrTokenUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up token: %w", err)
	}
	labels, err := s.q.ListProvisioningTokenLabels(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("store: listing token labels: %w", err)
	}
	t := tokenFromRow(row)
	for _, l := range labels {
		t.Labels = append(t.Labels, enroll.Label{Key: l.Key, Value: l.Value})
	}
	return &t, nil
}

// ListTokens implements enroll.Store.
func (s *Store) ListTokens(ctx context.Context) ([]enroll.Token, error) {
	rows, err := s.q.ListProvisioningTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing tokens: %w", err)
	}
	labels, err := s.q.ListAllProvisioningTokenLabels(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing token labels: %w", err)
	}
	byToken := make(map[uuid.UUID][]enroll.Label, len(rows))
	for _, l := range labels {
		byToken[l.TokenID] = append(byToken[l.TokenID], enroll.Label{Key: l.Key, Value: l.Value})
	}
	out := make([]enroll.Token, 0, len(rows))
	for _, row := range rows {
		t := tokenFromRow(row)
		t.Labels = byToken[row.ID]
		out = append(out, t)
	}
	return out, nil
}

// RevokeToken implements enroll.Store.
func (s *Store) RevokeToken(ctx context.Context, id uuid.UUID, now time.Time) error {
	n, err := s.q.RevokeProvisioningToken(ctx, db.RevokeProvisioningTokenParams{ID: id, RevokedAt: &now})
	if err != nil {
		return fmt.Errorf("store: revoking token: %w", err)
	}
	if n == 1 {
		return nil
	}
	// Nothing changed: either the token is unknown or already revoked.
	if _, err := s.q.GetProvisioningToken(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return enroll.ErrTokenUnknown
	} else if err != nil {
		return fmt.Errorf("store: looking up token: %w", err)
	}
	return enroll.ErrTokenRevoked
}

func tokenFromRow(row db.ProvisioningToken) enroll.Token {
	return enroll.Token{
		ID:         row.ID,
		Hash:       row.TokenHash,
		Name:       row.Name,
		Labels:     []enroll.Label{},
		CreatedAt:  row.CreatedAt,
		ExpiresAt:  row.ExpiresAt,
		RevokedAt:  row.RevokedAt,
		UseCount:   row.UseCount,
		LastUsedAt: row.LastUsedAt,
	}
}

// --- workloads -------------------------------------------------------------

// CreateWorkload implements enroll.Store.
func (s *Store) CreateWorkload(ctx context.Context, w enroll.Workload, now time.Time) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateWorkload(ctx, db.CreateWorkloadParams{
			ID:                  w.ID.UUID(),
			ProvisioningTokenID: w.TokenID,
			Hostname:            w.Hostname,
			EnrolledAt:          w.EnrolledAt,
			CredentialSerial:    w.CredentialSerial,
			CredentialExpiresAt: w.CredentialExpiresAt,
		}); err != nil {
			return fmt.Errorf("store: creating workload: %w", err)
		}
		for _, l := range w.Labels {
			if err := q.AddWorkloadLabel(ctx, db.AddWorkloadLabelParams{WorkloadID: w.ID.UUID(), Key: l.Key, Value: l.Value}); err != nil {
				return fmt.Errorf("store: adding workload label: %w", err)
			}
		}
		if err := q.RecordProvisioningTokenUse(ctx, db.RecordProvisioningTokenUseParams{ID: w.TokenID, LastUsedAt: &now}); err != nil {
			return fmt.Errorf("store: recording token use: %w", err)
		}
		return nil
	})
}

// GetWorkload implements enroll.Store.
func (s *Store) GetWorkload(ctx context.Context, id identity.WorkloadID) (*enroll.Workload, error) {
	row, err := s.q.GetWorkload(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, enroll.ErrWorkloadUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up workload: %w", err)
	}
	labels, err := s.q.ListWorkloadLabels(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload labels: %w", err)
	}
	w := &enroll.Workload{
		ID:                  identity.FromUUID(row.ID),
		TokenID:             row.ProvisioningTokenID,
		Hostname:            row.Hostname,
		Labels:              make([]enroll.Label, 0, len(labels)),
		EnrolledAt:          row.EnrolledAt,
		CredentialSerial:    row.CredentialSerial,
		CredentialExpiresAt: row.CredentialExpiresAt,
		LastRenewedAt:       row.LastRenewedAt,
	}
	for _, l := range labels {
		w.Labels = append(w.Labels, enroll.Label{Key: l.Key, Value: l.Value})
	}
	return w, nil
}

// RecordRenewal implements enroll.Store.
func (s *Store) RecordRenewal(ctx context.Context, id identity.WorkloadID, serial string, expiresAt, now time.Time) error {
	n, err := s.q.RecordWorkloadRenewal(ctx, db.RecordWorkloadRenewalParams{
		ID:                  id.UUID(),
		CredentialSerial:    serial,
		CredentialExpiresAt: expiresAt,
		LastRenewedAt:       &now,
	})
	if err != nil {
		return fmt.Errorf("store: recording renewal: %w", err)
	}
	if n == 0 {
		return enroll.ErrWorkloadUnknown
	}
	return nil
}

// tx runs fn inside one transaction.
func (s *Store) tx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: beginning transaction: %w", err)
	}
	if err := fn(s.q.WithTx(tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: committing transaction: %w", err)
	}
	return nil
}

// migrationsFS exposes the embedded migration files at their root so goose
// sees NNNNN_name.sql directly.
func migrationsFS() fs.FS {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the directory is embedded above; absence is a build error
	}
	return sub
}

package readmodel

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// Page limits for the fleet list.
const (
	DefaultWorkloadPageLimit = 100
	MaxWorkloadPageLimit     = 500
)

// WorkloadRecord is a workload as the store returns it for the read
// model: the registry's record, the latest rendered version and when it
// was rendered (zero and nil when none has been rendered), the listening
// services the agent reported, and the position keys the fleet order
// sorts by.
type WorkloadRecord struct {
	registry.Workload
	// LastRenewedAt is when the credential was last reissued; nil when
	// the enrollment credential is still the current one.
	LastRenewedAt     *time.Time
	LatestVersion     uint64
	LatestRenderedAt  *time.Time
	ListeningServices []registry.ListeningService
	// SyncRank and SeenKey are the store's sort keys for the fleet order:
	// the rank of the sync state (attention first) and the last-seen
	// instant with never-seen as the epoch. A cursor carries them.
	SyncRank int32
	SeenKey  time.Time
}

// WorkloadCursor is the position after which a fleet page continues.
type WorkloadCursor struct {
	SyncRank int32
	SeenKey  time.Time
	ID       identity.WorkloadID
}

// WorkloadPageQuery selects one page of the fleet. An empty IDs means
// every workload; a zero Mode or SyncState means any; a nil After is the
// first page.
type WorkloadPageQuery struct {
	IDs       []identity.WorkloadID
	Mode      innerwallv1.EnforcementMode
	SyncState innerwallv1.SyncState
	After     *WorkloadCursor
	Limit     int
}

// CredentialState is the state of a workload's credential as the stored
// expiry, the stored renewal error, and the clock give it (ADR-0016).
type CredentialState string

// Credential states.
const (
	// CredentialRenews: the credential is valid and the last automatic
	// renewal, if any was due, succeeded.
	CredentialRenews CredentialState = "renews"
	// CredentialRenewalFailed: the credential is still valid but the
	// agent's last renewal attempt failed; it is heading for expiry.
	CredentialRenewalFailed CredentialState = "renewal-failed"
	// CredentialExpired: the credential has lapsed; the workload must
	// re-enroll.
	CredentialExpired CredentialState = "expired"
)

// CredentialStatus is a workload's credential as the console shows it.
type CredentialStatus struct {
	State         CredentialState
	ExpiresAt     time.Time
	LastRenewedAt *time.Time
	LastError     string
}

// SyncStatus is a workload's convergence with its rendered policy: the
// state the stream reports, the version the agent acknowledged against
// the latest rendered one, and when that latest render happened.
type SyncStatus struct {
	State            innerwallv1.SyncState
	AppliedVersion   uint64
	LatestVersion    uint64
	LatestRenderedAt *time.Time
	Error            string
}

// Health is what the agent's heartbeats and renewals have recorded.
type Health struct {
	LastSeenAt         *time.Time
	Credential         CredentialStatus
	DroppedFlowRecords uint64
}

// Workload is the one object shape the fleet list and the workload detail
// share. The degraded-state screen reads entirely from it.
type Workload struct {
	ID                identity.WorkloadID
	Hostname          string
	Labels            []registry.Label
	Mode              innerwallv1.EnforcementMode
	EnrolledAt        time.Time
	Addresses         []netip.Addr
	Facts             *innerwallv1.HostFacts
	Agent             registry.AgentInfo
	ListeningServices []registry.ListeningService
	Sync              SyncStatus
	Health            Health
}

// WorkloadsRequest asks for one page of the fleet. An empty Selector
// means every workload, otherwise the workloads it currently matches; a
// zero Mode or SyncState means any; Cursor continues a previous page.
type WorkloadsRequest struct {
	Selector  policy.Selector
	Mode      innerwallv1.EnforcementMode
	SyncState innerwallv1.SyncState
	Cursor    string
	Limit     int
}

// WorkloadsPage is one page of the fleet in fleet order: sync state
// (degraded, offline, pending, synced), then most recently seen first.
type WorkloadsPage struct {
	Workloads  []Workload
	NextCursor string
}

const workloadsCursorKind = "w1"

func encodeWorkloadCursor(c WorkloadCursor) string {
	return encodeCursor(workloadsCursorKind, strconv.FormatInt(int64(c.SyncRank), 10), strconv.FormatInt(c.SeenKey.UnixNano(), 10), c.ID.String())
}

func decodeWorkloadCursor(token string) (*WorkloadCursor, error) {
	if token == "" {
		return nil, nil //nolint:nilnil // the first page has no cursor
	}
	parts, err := decodeCursor(token, workloadsCursorKind, 3)
	if err != nil {
		return nil, err
	}
	rank, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	seen, err := cursorTime(parts[1])
	if err != nil {
		return nil, err
	}
	id, err := cursorUUID(parts[2])
	if err != nil {
		return nil, err
	}
	return &WorkloadCursor{SyncRank: int32(rank), SeenKey: seen, ID: identity.FromUUID(id)}, nil
}

// ListWorkloads returns one page of the fleet.
func (s *Reader) ListWorkloads(ctx context.Context, req WorkloadsRequest) (*WorkloadsPage, error) {
	after, err := decodeWorkloadCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultWorkloadPageLimit
	}
	if limit > MaxWorkloadPageLimit {
		limit = MaxWorkloadPageLimit
	}
	page := &WorkloadsPage{Workloads: []Workload{}}
	ids, scoped, err := s.scope(ctx, nil, req.Selector)
	if err != nil {
		return nil, err
	}
	if scoped && len(ids) == 0 {
		return page, nil
	}
	records, err := s.Store.ListWorkloadPage(ctx, WorkloadPageQuery{IDs: ids, Mode: req.Mode, SyncState: req.SyncState, After: after, Limit: limit + 1})
	if err != nil {
		return nil, err
	}
	more := len(records) > limit
	if more {
		records = records[:limit]
	}
	now := s.now()
	for i := range records {
		page.Workloads = append(page.Workloads, s.workload(&records[i], now))
	}
	if more {
		last := &records[len(records)-1]
		page.NextCursor = encodeWorkloadCursor(WorkloadCursor{SyncRank: last.SyncRank, SeenKey: last.SeenKey, ID: last.ID})
	}
	return page, nil
}

// GetWorkload returns one workload, or registry.ErrWorkloadUnknown.
func (s *Reader) GetWorkload(ctx context.Context, id identity.WorkloadID) (*Workload, error) {
	rec, err := s.Store.GetWorkloadRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	w := s.workload(rec, s.now())
	return &w, nil
}

func (s *Reader) workload(rec *WorkloadRecord, now time.Time) Workload {
	w := Workload{
		ID: rec.ID, Hostname: rec.Hostname, Labels: rec.Labels, Mode: rec.Mode, EnrolledAt: rec.EnrolledAt,
		Addresses: rec.Addresses, Facts: rec.Facts, Agent: rec.Agent, ListeningServices: rec.ListeningServices,
		Sync: SyncStatus{State: rec.SyncState, AppliedVersion: rec.AppliedVersion, LatestVersion: rec.LatestVersion, LatestRenderedAt: rec.LatestRenderedAt, Error: rec.SyncError},
		Health: Health{
			LastSeenAt:         rec.LastSeenAt,
			Credential:         credentialStatus(rec, now),
			DroppedFlowRecords: rec.DroppedFlowRecords,
		},
	}
	if w.Labels == nil {
		w.Labels = []registry.Label{}
	}
	if w.Addresses == nil {
		w.Addresses = []netip.Addr{}
	}
	if w.ListeningServices == nil {
		w.ListeningServices = []registry.ListeningService{}
	}
	if w.Agent.Capabilities == nil {
		w.Agent.Capabilities = []string{}
	}
	return w
}

// credentialStatus derives the credential state: expired once the stored
// expiry has passed, renewal-failed while the heartbeat's last renewal
// error stands, renews otherwise.
func credentialStatus(rec *WorkloadRecord, now time.Time) CredentialStatus {
	st := CredentialStatus{State: CredentialRenews, ExpiresAt: rec.CredentialExpiresAt, LastRenewedAt: rec.LastRenewedAt, LastError: rec.CredentialRenewalError}
	switch {
	case !now.Before(rec.CredentialExpiresAt):
		st.State = CredentialExpired
	case rec.CredentialRenewalError != "":
		st.State = CredentialRenewalFailed
	}
	return st
}

// IsUnknown reports whether err says a workload is not registered.
func IsUnknown(err error) bool {
	return errors.Is(err, registry.ErrWorkloadUnknown)
}

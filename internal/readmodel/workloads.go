package readmodel

import (
	"context"
	"errors"
	"fmt"
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
	// the attention rank and the last-seen instant with never-seen as the
	// epoch. A cursor carries them.
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
// the latest rendered one, when that latest render happened, and when the
// sync path last sent the workload a snapshot.
type SyncStatus struct {
	State            innerwallv1.SyncState
	AppliedVersion   uint64
	LatestVersion    uint64
	LatestRenderedAt *time.Time
	Error            string
	// LastSnapshotSentAt is nil when no snapshot has been recorded. A
	// directed reconnect is observed as this instant advancing.
	LastSnapshotSentAt *time.Time
	// LastAckedAt is when the agent last acknowledged an applied
	// version, and LastApplyFailedAt when it last reported a failed
	// apply; each nil when none has been recorded. A failure instant
	// outlives the failure: State says whether it still stands.
	LastAckedAt       *time.Time
	LastApplyFailedAt *time.Time
}

// Health is what the agent's heartbeats and renewals have recorded.
type Health struct {
	LastSeenAt         *time.Time
	Credential         CredentialStatus
	DroppedFlowRecords uint64
	// SourceOverruns counts the moments the kernel dropped events because
	// one of the agent's flow sources fell behind, since the agent
	// started; each is also an evidence gap with its interval.
	SourceOverruns uint64
	// ClockSkew is the latest flow report whose agent clock was beyond
	// the tolerance, and whether it is still the latest reading; nil when
	// none has been received.
	ClockSkew *registry.ClockSkew
	// OwnedTable is whether the agent's owned firewall table is in the
	// kernel, as its heartbeats last said; unspecified until one that
	// knows has arrived. OwnedTableAbsentSince is when the control plane
	// first heard it was absent, nil unless it is. An absent table means
	// the host enforces and observes nothing, whatever the sync state
	// says of the version the agent acknowledged (issue #64).
	OwnedTable            innerwallv1.OwnedTableState
	OwnedTableAbsentSince *time.Time
	// LastFlowReportAt is when the control plane last received a flow
	// report from the agent; nil when none has been. TelemetryLive says
	// one arrived within TelemetryLiveWindows flow windows of the read: the
	// agent is running and reaching the control plane, whatever its sync
	// stream's state, which travels on another connection (issue #61).
	LastFlowReportAt *time.Time
	TelemetryLive    bool
}

// TelemetryLiveWindows is how many flow windows may pass since the last
// report while a workload's telemetry is still live. The reporter ships
// one window per window length; two allow one late report.
const TelemetryLiveWindows = 2

// telemetryLive says a report arrived within TelemetryLiveWindows flow
// windows of now; never with no window to judge by.
func telemetryLive(last *time.Time, window time.Duration, now time.Time) bool {
	return window > 0 && last != nil && now.Sub(*last) <= TelemetryLiveWindows*window
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

// WorkloadsPage is one page of the fleet in fleet order: attention rank
// (degraded or enforcement removed, offline, pending, synced), then most
// recently seen first.
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

// WorkloadCounts is the fleet's size, in total, by sync state, and
// removed. A workload whose owned table is recorded absent counts in
// Removed and in no sync state: its stored state is the agent's honest
// record of the version it acknowledged, but the host enforces none of it
// (issue #96). Every other workload is in exactly one sync state, so Total
// is the sum of BySync and Removed.
type WorkloadCounts struct {
	Total   int64
	BySync  map[innerwallv1.SyncState]int64
	Removed int64
}

// StatusCount is how many workloads hold one sync state with their owned
// table recorded absent (Removed), or not.
type StatusCount struct {
	SyncState innerwallv1.SyncState
	Removed   bool
	Workloads int64
}

// syncStates are the states a workload can hold, each counted even when
// no workload holds it.
var syncStates = []innerwallv1.SyncState{
	innerwallv1.SyncState_SYNC_STATE_SYNCED,
	innerwallv1.SyncState_SYNC_STATE_PENDING,
	innerwallv1.SyncState_SYNC_STATE_DEGRADED,
	innerwallv1.SyncState_SYNC_STATE_OFFLINE,
}

// CountWorkloads returns how many workloads the fleet holds, in total,
// in each sync state, and removed, from one read of the store, so the
// counts agree with each other. A count is exact or the read fails: a
// workload stored in a state outside the four is an error, removed or
// not, never left out of a total.
func (s *Reader) CountWorkloads(ctx context.Context) (*WorkloadCounts, error) {
	stored, err := s.Store.CountWorkloadsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := &WorkloadCounts{BySync: make(map[innerwallv1.SyncState]int64, len(syncStates))}
	for _, st := range syncStates {
		out.BySync[st] = 0
	}
	for _, c := range stored {
		if c.Workloads == 0 {
			continue
		}
		if _, known := out.BySync[c.SyncState]; !known {
			return nil, fmt.Errorf("readmodel: %d workloads are stored in unknown sync state %d", c.Workloads, c.SyncState)
		}
		if c.Removed {
			out.Removed += c.Workloads
		} else {
			out.BySync[c.SyncState] += c.Workloads
		}
		out.Total += c.Workloads
	}
	return out, nil
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
		Sync: SyncStatus{State: rec.SyncState, AppliedVersion: rec.AppliedVersion, LatestVersion: rec.LatestVersion, LatestRenderedAt: rec.LatestRenderedAt, Error: rec.SyncError, LastSnapshotSentAt: rec.LastSnapshotSentAt,
			LastAckedAt: rec.LastAckedAt, LastApplyFailedAt: rec.LastApplyFailedAt},
		Health: Health{
			LastSeenAt:            rec.LastSeenAt,
			Credential:            credentialStatus(rec, now),
			DroppedFlowRecords:    rec.DroppedFlowRecords,
			SourceOverruns:        rec.SourceOverruns,
			ClockSkew:             rec.ClockSkew,
			OwnedTable:            rec.OwnedTable,
			OwnedTableAbsentSince: rec.OwnedTableAbsentSince,
			LastFlowReportAt:      rec.LastFlowReportAt,
			TelemetryLive:         telemetryLive(rec.LastFlowReportAt, s.FlowWindow, now),
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

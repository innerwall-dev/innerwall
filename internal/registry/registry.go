package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// ErrWorkloadUnknown is returned when an id is not registered.
var ErrWorkloadUnknown = errors.New("registry: workload is not registered")

// ErrWorkloadRetired is returned when an id names a workload the operator
// has retired (ADR-0023). The error carrying it is a *RetiredError naming
// the instant. It is for the operator and the logs: an agent presenting a
// retired identity is told nothing but that its credential is refused.
var ErrWorkloadRetired = errors.New("registry: workload is retired")

// RetiredError is a refusal naming the workload and when it was retired.
type RetiredError struct {
	ID        identity.WorkloadID
	RetiredAt time.Time
}

func (e *RetiredError) Error() string {
	return fmt.Sprintf("registry: workload %s was retired at %s", e.ID, e.RetiredAt.UTC().Format(time.RFC3339))
}

func (e *RetiredError) Unwrap() error { return ErrWorkloadRetired }

// Label is one key/value pair on a workload.
type Label struct {
	Key   string
	Value string
}

// ListeningService is a service the agent reported as listening.
type ListeningService struct {
	Protocol    innerwallv1.Protocol
	Port        uint32
	ProcessName string
	ProcessPath string
}

// AgentInfo is what the agent said about itself in Hello.
type AgentInfo struct {
	Version      string
	Capabilities []string
}

// Workload is the control plane's record of one workload: identity plus
// everything mutable that lives beside it rather than in the credential.
type Workload struct {
	ID                 identity.WorkloadID
	Hostname           string
	Labels             []Label
	Mode               innerwallv1.EnforcementMode
	Facts              *innerwallv1.HostFacts
	Addresses          []netip.Addr
	Agent              AgentInfo
	EnrolledAt         time.Time
	LastSeenAt         *time.Time
	SyncState          innerwallv1.SyncState
	AppliedVersion     uint64
	SyncError          string
	DroppedFlowRecords uint64
	// SourceOverruns is the kernel-side overruns of the agent's flow
	// sources since it started, as its heartbeat reported them.
	SourceOverruns uint64
	// CredentialRenewalError is the reason the agent's last automatic
	// renewal failed, as its heartbeat reported it; empty when healthy.
	CredentialRenewalError string
	// CredentialSerial and CredentialExpiresAt are the credential the
	// control plane last issued the workload.
	CredentialSerial    string
	CredentialExpiresAt time.Time
	// PresentedCredential is the credential the workload's most recent
	// sync stream opened on; nil when no stream has opened since the
	// control plane began recording it. A stream ending leaves it, so it
	// is the credential in use only while SyncState says a stream is
	// open; after a renewal the live stream keeps the one renewal
	// replaced until its end. Written at stream open alone.
	PresentedCredential *PresentedCredential
	// LateRenewal is the latest renewal made with an already-expired
	// credential, within the renewal grace; nil when the workload has
	// never renewed late. Written by renewal alone (ADR-0016 as amended).
	LateRenewal *LateRenewal
	// LastSnapshotSentAt is when the sync path last sent this workload a
	// snapshot, whatever caused it; nil when none has been recorded. It
	// is a record of what the control plane did, written by the sync
	// path alone.
	LastSnapshotSentAt *time.Time
	// LastAckedAt is when the agent last acknowledged an applied
	// version; nil when none has been recorded. The version a Hello
	// claims is not an acknowledgement.
	LastAckedAt *time.Time
	// LastApplyFailedAt is when the agent last reported a failed apply;
	// nil when none has been recorded. A later successful apply leaves
	// it: SyncState says whether the workload is still degraded.
	LastApplyFailedAt *time.Time
	// ClockSkew is the latest flow report whose agent clock was beyond
	// the tolerance; nil when none has been received. Written by flow
	// ingest alone.
	ClockSkew *ClockSkew
	// OwnedTable is whether the agent's owned firewall table is in the
	// kernel, as its heartbeats last said: unspecified until a heartbeat
	// that knows has arrived. OwnedTableAbsentSince is when the control
	// plane first heard it was absent, nil unless it is.
	OwnedTable            innerwallv1.OwnedTableState
	OwnedTableAbsentSince *time.Time
	// LastFlowReportAt is when the control plane last received a flow
	// report from the agent; nil when none has been. Flow reports travel
	// on a connection of their own (ADR-0015), so this can move while the
	// sync stream is down. Written by flow ingest alone.
	LastFlowReportAt *time.Time
	// RetiredAt is when the operator retired the workload; nil while it
	// is active. Retirement is one way: the row is kept so history keeps
	// naming the identity, and nothing that acts on the present (a
	// render, a selector, ingest's peer resolution) sees it (ADR-0023).
	RetiredAt *time.Time
}

// Retired says the operator has retired the workload.
func (w *Workload) Retired() bool { return w.RetiredAt != nil }

// Active returns the workloads that have not been retired, in order.
func Active(workloads []Workload) []Workload {
	out := make([]Workload, 0, len(workloads))
	for i := range workloads {
		if !workloads[i].Retired() {
			out = append(out, workloads[i])
		}
	}
	return out
}

// Heartbeat is what one heartbeat reports.
type Heartbeat struct {
	DroppedFlowRecords uint64
	SourceOverruns     uint64
	RenewalError       string
	// OwnedTable is the owned table's state as the agent read it; an
	// unspecified state leaves the recorded one as it was.
	OwnedTable innerwallv1.OwnedTableState
	// Uptime is how long the agent process had run when it sent the
	// heartbeat, on its own monotonic clock. With the receipt instant it
	// places the process start on the control plane's clock.
	Uptime time.Duration
}

// AgentProcess is what a workload's latest heartbeat proved about its
// agent: the process that started at StartedAt was still running when the
// heartbeat arrived at HeartbeatAt, both on the control plane's clock. One
// process ran throughout, so nothing in between was a restart. StartedAt
// is the receipt minus the reported uptime, late by the heartbeat's
// transit and the uptime's truncation, never early.
type AgentProcess struct {
	StartedAt   time.Time
	HeartbeatAt time.Time
}

// PresentedCredential is the certificate a sync stream opened on, as the
// interceptor verified it: its serial, in the form issuance records
// (enroll.CredentialSerial), and its expiry.
type PresentedCredential struct {
	Serial    string
	ExpiresAt time.Time
}

// LateRenewal is a renewal made with a credential that had already
// expired, within the renewal grace: when it happened and when the
// credential it replaced had expired. The difference is how late it was.
type LateRenewal struct {
	RenewedAt time.Time
	ExpiredAt time.Time
}

// ClockSkew is a skewed reading of an agent's clock from its flow
// reports: the windows it stamps land at the wrong times.
type ClockSkew struct {
	// Offset is the agent's clock minus the control plane's in the latest
	// skewed report: negative when the agent is behind.
	Offset time.Duration
	// ObservedAt is when that report was received.
	ObservedAt time.Time
	// Current says the latest report received was skewed; false once a
	// report within tolerance has followed it.
	Current bool
}

// ErrLabelsChanged is returned by a conditional label write whose expected
// version is not the labels' current one. The error carrying it is a
// *LabelsVersionError naming the current version.
var ErrLabelsChanged = errors.New("registry: the workload's labels have changed since they were read")

// LabelsVersionError is a refused conditional label write with the
// version the labels hold now.
type LabelsVersionError struct {
	Current string
}

func (e *LabelsVersionError) Error() string {
	return ErrLabelsChanged.Error() + " (current version " + e.Current + ")"
}

func (e *LabelsVersionError) Unwrap() error { return ErrLabelsChanged }

// LabelsVersion is the version of a workload's labels: a digest of the
// set itself, since nothing persisted records when labels last changed
// and the set is the whole of what an operator edits. Two label sets that
// say the same thing have the same version, whatever order they were
// written in.
func LabelsVersion(labels []Label) string {
	pairs := make([]string, 0, len(labels))
	for _, l := range labels {
		pairs = append(pairs, l.Key+"="+l.Value)
	}
	sort.Strings(pairs)
	h := sha256.New()
	for _, p := range pairs {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// LabelMap returns the labels as a map for selector matching.
func (w *Workload) LabelMap() map[string]string {
	m := make(map[string]string, len(w.Labels))
	for _, l := range w.Labels {
		m[l.Key] = l.Value
	}
	return m
}

// Store is the persistence the registry needs, implemented with
// hand-written SQL in internal/store (ADR-0006). Methods that take an id
// return ErrWorkloadUnknown when it is not registered. The writes a live
// sync stream makes from what its agent sends (RecordAgent, RecordFacts,
// RecordListeningServices, RecordHeartbeat, RecordApplied, and
// RecordApplyFailed) are conditioned on the workload not being retired and
// return a *RetiredError for one that is, so a stream of a retired
// workload ends at its next such write whatever else reached it
// (ADR-0023).
type Store interface {
	ListWorkloads(ctx context.Context) ([]Workload, error)
	LookupWorkload(ctx context.Context, id identity.WorkloadID) (*Workload, error)
	ListListeningServices(ctx context.Context, id identity.WorkloadID) ([]ListeningService, error)

	// SetWorkloadLabels replaces the workload's labels.
	SetWorkloadLabels(ctx context.Context, id identity.WorkloadID, labels []Label) error
	// SetWorkloadMode changes the enforcement mode.
	SetWorkloadMode(ctx context.Context, id identity.WorkloadID, mode innerwallv1.EnforcementMode) error

	// RecordFacts stores host facts and refreshes last-seen. It reports
	// whether the set of addresses derived from the facts changed, which is
	// a render trigger because peers resolve to those addresses.
	RecordFacts(ctx context.Context, id identity.WorkloadID, facts *innerwallv1.HostFacts, now time.Time) (addressesChanged bool, err error)
	// RecordListeningServices replaces the workload's listening services.
	RecordListeningServices(ctx context.Context, id identity.WorkloadID, services []ListeningService, now time.Time) error
	// RecordAgent stores what a Hello said: agent info and the version the
	// agent claims to have applied; and the credential the stream opened
	// on, which a nil presented leaves as it was.
	RecordAgent(ctx context.Context, id identity.WorkloadID, agent AgentInfo, appliedVersion uint64, presented *PresentedCredential, now time.Time) error
	// RecordHeartbeat refreshes last-seen and what the heartbeat carries:
	// the loss counters, the renewal status, the owned table's state, and
	// the agent process it came from.
	RecordHeartbeat(ctx context.Context, id identity.WorkloadID, hb Heartbeat, now time.Time) error
	// SetSyncState records the convergence state with an optional detail.
	SetSyncState(ctx context.Context, id identity.WorkloadID, state innerwallv1.SyncState, detail string, now time.Time) error
	// RecordApplied stores an acknowledged version and the resulting
	// state, and stamps now as the last acknowledgement.
	RecordApplied(ctx context.Context, id identity.WorkloadID, version uint64, state innerwallv1.SyncState, now time.Time) error
	// RecordApplyFailed stores a failed apply: the degraded state and the
	// agent's detail, and stamps now as the last apply failure. The
	// applied version is untouched.
	RecordApplyFailed(ctx context.Context, id identity.WorkloadID, detail string, now time.Time) error
	// RecordSnapshotSent stamps the instant the sync path sent the
	// workload a snapshot. The sync path is its only caller.
	RecordSnapshotSent(ctx context.Context, id identity.WorkloadID, at time.Time) error
}

// AddressesFromFacts derives the addresses a workload can be reached at
// from its reported interfaces: every unicast address that is not
// loopback, link-local, multicast, or unspecified, in sorted order without
// duplicates. These become host routes in peers' rendered policies. An
// address that does not parse is skipped; facts are descriptive input from
// the host and never trusted to be well-formed.
func AddressesFromFacts(facts *innerwallv1.HostFacts) []netip.Addr {
	seen := map[netip.Addr]struct{}{}
	var out []netip.Addr
	for _, iface := range facts.GetInterfaces() {
		for _, s := range iface.GetAddresses() {
			addr, ok := parseAddr(s)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() || addr.IsInterfaceLocalMulticast() {
				continue
			}
			if _, dup := seen[addr]; dup {
				continue
			}
			seen[addr] = struct{}{}
			out = append(out, addr)
		}
	}
	sortAddrs(out)
	return out
}

func parseAddr(s string) (netip.Addr, bool) {
	if pfx, err := netip.ParsePrefix(s); err == nil {
		return pfx.Addr(), true
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		return addr, true
	}
	return netip.Addr{}, false
}

func sortAddrs(a []netip.Addr) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j].Less(a[j-1]); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

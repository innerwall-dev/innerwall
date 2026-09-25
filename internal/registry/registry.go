package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"sort"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// ErrWorkloadUnknown is returned when an id is not registered.
var ErrWorkloadUnknown = errors.New("registry: workload is not registered")

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
	// CredentialRenewalError is the reason the agent's last automatic
	// renewal failed, as its heartbeat reported it; empty when healthy.
	CredentialRenewalError string
	CredentialExpiresAt    time.Time
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
// return ErrWorkloadUnknown when it is not registered.
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
	// agent claims to have applied.
	RecordAgent(ctx context.Context, id identity.WorkloadID, agent AgentInfo, appliedVersion uint64, now time.Time) error
	// RecordHeartbeat refreshes last-seen, the dropped-flow counter, and
	// the renewal status the heartbeat carries.
	RecordHeartbeat(ctx context.Context, id identity.WorkloadID, droppedFlowRecords uint64, renewalError string, now time.Time) error
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

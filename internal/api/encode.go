package api

import (
	"net/netip"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// The JSON shapes of the read endpoints. Field names are snake_case and
// timestamps are RFC 3339 in UTC (ADR-0021); enumerations are the names
// the command line prints. These are encodings of the read model's
// types and hold nothing the read model does not.

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func optionalTimestamp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := timestamp(*t)
	return &s
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type countersJSON struct {
	FlowCount       int64  `json:"flow_count"`
	ConnectionCount uint64 `json:"connection_count"`
	ByteCount       uint64 `json:"byte_count"`
}

func counters(c readmodel.Counters) countersJSON {
	return countersJSON{FlowCount: c.FlowCount, ConnectionCount: c.ConnectionCount, ByteCount: c.ByteCount}
}

type serviceJSON struct {
	Protocol string `json:"protocol"`
	Port     uint16 `json:"port"`
}

func service(s readmodel.Service) serviceJSON {
	return serviceJSON{Protocol: policy.ProtocolName(s.Protocol), Port: s.Port}
}

type ruleJSON struct {
	ID             string `json:"id"`
	AuthoredRuleID string `json:"authored_rule_id,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
}

func rule(r *readmodel.RuleRef) *ruleJSON {
	if r == nil {
		return nil
	}
	out := &ruleJSON{ID: r.ID, AuthoredRuleID: r.AuthoredRuleID}
	if r.Protocol != innerwallv1.Protocol_PROTOCOL_UNSPECIFIED {
		out.Protocol = policy.ProtocolName(r.Protocol)
	}
	return out
}

// peerJSON is a resolved peer as stored at ingest: its kind, the identity
// behind it (a workload id, an address group id, or the bare address),
// the name the registry currently gives that identity, and the label
// snapshot captured at the time for a workload peer.
type peerJSON struct {
	Kind           string            `json:"kind"`
	WorkloadID     string            `json:"workload_id,omitempty"`
	AddressGroupID string            `json:"address_group_id,omitempty"`
	Address        string            `json:"address,omitempty"`
	Name           string            `json:"name,omitempty"`
	Labels         map[string]string `json:"labels"`
}

func peer(p *readmodel.PeerRef) *peerJSON {
	if p == nil {
		return nil
	}
	out := &peerJSON{Kind: p.Kind.String(), Name: p.Name, Labels: p.Labels}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	switch p.Kind {
	case flowstore.PeerWorkload:
		out.WorkloadID = p.Key
	case flowstore.PeerAddressGroup:
		out.AddressGroupID = p.Key
	case flowstore.PeerUnknown:
		out.Address = p.Key
	default:
		out.Address = p.Key
	}
	return out
}

type workloadRefJSON struct {
	ID       string            `json:"id"`
	Hostname string            `json:"hostname"`
	Labels   map[string]string `json:"labels"`
}

func workloadRef(w *readmodel.WorkloadRef) *workloadRefJSON {
	if w == nil {
		return nil
	}
	labels := w.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return &workloadRefJSON{ID: w.ID.String(), Hostname: w.Hostname, Labels: labels}
}

// --- rollup ------------------------------------------------------------------

type rollupGroupJSON struct {
	Keys map[string]any `json:"keys"`
	countersJSON
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

type rollupResponse struct {
	From          string            `json:"from"`
	To            string            `json:"to"`
	EffectiveFrom *string           `json:"effective_from"`
	EffectiveTo   *string           `json:"effective_to"`
	GroupBy       []string          `json:"group_by"`
	Groups        []rollupGroupJSON `json:"groups"`
	GroupCount    int64             `json:"group_count"`
	Truncated     bool              `json:"truncated"`
	Totals        countersJSON      `json:"totals"`
}

func rollupJSON(r *readmodel.Rollup) rollupResponse {
	out := rollupResponse{
		From: timestamp(r.Range.From), To: timestamp(r.Range.To),
		EffectiveFrom: optionalTimestamp(r.EffectiveFrom), EffectiveTo: optionalTimestamp(r.EffectiveTo),
		GroupBy: r.GroupBy.Keys(), Groups: make([]rollupGroupJSON, 0, len(r.Groups)),
		GroupCount: r.GroupCount, Truncated: r.Truncated, Totals: counters(r.Totals),
	}
	for i := range r.Groups {
		g := &r.Groups[i]
		keys := make(map[string]any, len(out.GroupBy))
		for _, k := range out.GroupBy {
			switch k {
			case "rule":
				keys[k] = rule(g.Keys.Rule)
			case "peer":
				keys[k] = peer(g.Keys.Peer)
			case "src":
				keys[k] = peer(g.Keys.Src)
			case "dst":
				keys[k] = workloadRef(g.Keys.Dst)
			case "service":
				if g.Keys.Service != nil {
					keys[k] = service(*g.Keys.Service)
				} else {
					keys[k] = nil
				}
			}
		}
		out.Groups = append(out.Groups, rollupGroupJSON{Keys: keys, countersJSON: counters(g.Counters), FirstSeen: timestamp(g.FirstSeen), LastSeen: timestamp(g.LastSeen)})
	}
	return out
}

// --- flows -------------------------------------------------------------------

type flowJSON struct {
	ID              int64       `json:"id"`
	WindowStart     string      `json:"window_start"`
	WindowEnd       string      `json:"window_end"`
	Peer            *peerJSON   `json:"peer"`
	SrcAddress      string      `json:"src_address"`
	DstAddress      string      `json:"dst_address"`
	Service         serviceJSON `json:"service"`
	Direction       string      `json:"direction"`
	Verdict         string      `json:"verdict"`
	Rule            *ruleJSON   `json:"rule"`
	ConnectionCount uint64      `json:"connection_count"`
	ByteCount       uint64      `json:"byte_count"`
	FirstSeen       string      `json:"first_seen"`
	LastSeen        string      `json:"last_seen"`
	ProcessName     string      `json:"process_name,omitempty"`
}

type flowsResponse struct {
	Workload   *workloadRefJSON `json:"workload"`
	From       string           `json:"from"`
	To         string           `json:"to"`
	Flows      []flowJSON       `json:"flows"`
	NextCursor *string          `json:"next_cursor"`
}

func flowsPageJSON(p *readmodel.FlowsPage) flowsResponse {
	out := flowsResponse{Workload: workloadRef(&p.Workload), From: timestamp(p.Range.From), To: timestamp(p.Range.To), Flows: make([]flowJSON, 0, len(p.Flows)), NextCursor: optionalString(p.NextCursor)}
	for i := range p.Flows {
		f := &p.Flows[i]
		out.Flows = append(out.Flows, flowJSON{
			ID: f.ID, WindowStart: timestamp(f.WindowStart), WindowEnd: timestamp(f.WindowEnd),
			Peer: peer(&f.Peer), SrcAddress: addr(f.SrcAddress), DstAddress: addr(f.DstAddress),
			Service: service(f.Service), Direction: policy.DirectionName(f.Direction), Verdict: readmodel.VerdictName(f.Verdict),
			Rule: rule(f.Rule), ConnectionCount: f.ConnectionCount, ByteCount: f.ByteCount,
			FirstSeen: timestamp(f.FirstSeen), LastSeen: timestamp(f.LastSeen), ProcessName: f.ProcessName,
		})
	}
	return out
}

func addr(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	return a.String()
}

// --- workloads ---------------------------------------------------------------

type osJSON struct {
	Family        string `json:"family"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	KernelVersion string `json:"kernel_version"`
	Architecture  string `json:"architecture"`
}

type agentJSON struct {
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

type listeningServiceJSON struct {
	Protocol    string `json:"protocol"`
	Port        uint32 `json:"port"`
	ProcessName string `json:"process_name"`
	ProcessPath string `json:"process_path"`
}

type syncJSON struct {
	State            string  `json:"state"`
	AppliedVersion   uint64  `json:"applied_version"`
	LatestVersion    uint64  `json:"latest_version"`
	LatestRenderedAt *string `json:"latest_rendered_at"`
	Error            string  `json:"error"`
	// LastSnapshotSentAt is when the sync path last sent a snapshot.
	LastSnapshotSentAt *string `json:"last_snapshot_sent_at"`
}

type credentialJSON struct {
	State         string  `json:"state"`
	ExpiresAt     string  `json:"expires_at"`
	LastRenewedAt *string `json:"last_renewed_at"`
	LastError     string  `json:"last_error"`
}

type healthJSON struct {
	LastSeenAt         *string        `json:"last_seen_at"`
	Credential         credentialJSON `json:"credential"`
	DroppedFlowRecords uint64         `json:"dropped_flow_records"`
}

type workloadResponse struct {
	ID                string                 `json:"id"`
	Hostname          string                 `json:"hostname"`
	Labels            map[string]string      `json:"labels"`
	Mode              string                 `json:"mode"`
	EnrolledAt        string                 `json:"enrolled_at"`
	Addresses         []string               `json:"addresses"`
	OS                *osJSON                `json:"os"`
	Agent             agentJSON              `json:"agent"`
	ListeningServices []listeningServiceJSON `json:"listening_services"`
	Sync              syncJSON               `json:"sync"`
	Health            healthJSON             `json:"health"`
}

func labelMap(labels []registry.Label) map[string]string {
	m := make(map[string]string, len(labels))
	for _, l := range labels {
		m[l.Key] = l.Value
	}
	return m
}

func workloadJSON(w *readmodel.Workload) workloadResponse {
	out := workloadResponse{
		ID: w.ID.String(), Hostname: w.Hostname, Labels: labelMap(w.Labels), Mode: policy.ModeName(w.Mode), EnrolledAt: timestamp(w.EnrolledAt),
		Addresses:         make([]string, 0, len(w.Addresses)),
		Agent:             agentJSON{Version: w.Agent.Version, Capabilities: w.Agent.Capabilities},
		ListeningServices: make([]listeningServiceJSON, 0, len(w.ListeningServices)),
		Sync: syncJSON{State: readmodel.SyncStateName(w.Sync.State), AppliedVersion: w.Sync.AppliedVersion, LatestVersion: w.Sync.LatestVersion,
			LatestRenderedAt: optionalTimestamp(w.Sync.LatestRenderedAt), Error: w.Sync.Error, LastSnapshotSentAt: optionalTimestamp(w.Sync.LastSnapshotSentAt)},
		Health: healthJSON{
			LastSeenAt: optionalTimestamp(w.Health.LastSeenAt),
			Credential: credentialJSON{State: string(w.Health.Credential.State), ExpiresAt: timestamp(w.Health.Credential.ExpiresAt),
				LastRenewedAt: optionalTimestamp(w.Health.Credential.LastRenewedAt), LastError: w.Health.Credential.LastError},
			DroppedFlowRecords: w.Health.DroppedFlowRecords,
		},
	}
	if out.Agent.Capabilities == nil {
		out.Agent.Capabilities = []string{}
	}
	for _, a := range w.Addresses {
		out.Addresses = append(out.Addresses, a.String())
	}
	if os := w.Facts.GetOs(); os != nil {
		out.OS = &osJSON{Family: os.GetFamily(), Name: os.GetName(), Version: os.GetVersion(), KernelVersion: os.GetKernelVersion(), Architecture: os.GetArchitecture()}
	}
	for _, s := range w.ListeningServices {
		out.ListeningServices = append(out.ListeningServices, listeningServiceJSON{Protocol: policy.ProtocolName(s.Protocol), Port: s.Port, ProcessName: s.ProcessName, ProcessPath: s.ProcessPath})
	}
	return out
}

type workloadsResponse struct {
	Workloads  []workloadResponse `json:"workloads"`
	NextCursor *string            `json:"next_cursor"`
}

func workloadsPageJSON(p *readmodel.WorkloadsPage) workloadsResponse {
	out := workloadsResponse{Workloads: make([]workloadResponse, 0, len(p.Workloads)), NextCursor: optionalString(p.NextCursor)}
	for i := range p.Workloads {
		out.Workloads = append(out.Workloads, workloadJSON(&p.Workloads[i]))
	}
	return out
}

// --- rendered policy -------------------------------------------------------

type portRangeJSON struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

type rulesetRefJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type renderedRuleJSON struct {
	ID             string          `json:"id"`
	AuthoredRuleID string          `json:"authored_rule_id,omitempty"`
	Ruleset        *rulesetRefJSON `json:"ruleset"`
	Description    string          `json:"description"`
	CreatedAt      *string         `json:"created_at"`
	UpdatedAt      *string         `json:"updated_at"`
	Protocol       string          `json:"protocol"`
	Ports          []portRangeJSON `json:"ports"`
	PeerCIDRs      []string        `json:"peer_cidrs"`
	Verdict        string          `json:"verdict"`
}

type renderedPolicyResponse struct {
	Workload        *workloadRefJSON   `json:"workload"`
	Version         uint64             `json:"version"`
	Mode            string             `json:"mode"`
	RenderedAt      *string            `json:"rendered_at"`
	TerminalVerdict string             `json:"terminal_verdict"`
	Rules           []renderedRuleJSON `json:"rules"`
}

func renderedPolicyJSON(p *readmodel.RenderedPolicy) renderedPolicyResponse {
	out := renderedPolicyResponse{
		Workload: workloadRef(&p.Workload), Version: p.Version, Mode: policy.ModeName(p.Mode), RenderedAt: optionalTimestamp(p.RenderedAt),
		TerminalVerdict: readmodel.VerdictName(p.TerminalVerdict), Rules: make([]renderedRuleJSON, 0, len(p.Rules)),
	}
	for i := range p.Rules {
		r := &p.Rules[i]
		rj := renderedRuleJSON{ID: r.ID, AuthoredRuleID: r.AuthoredRuleID, Description: r.Description, CreatedAt: optionalTimestamp(r.CreatedAt), UpdatedAt: optionalTimestamp(r.UpdatedAt),
			Protocol: policy.ProtocolName(r.Protocol), Ports: make([]portRangeJSON, 0, len(r.Ports)), PeerCIDRs: r.PeerCIDRs, Verdict: readmodel.VerdictName(r.Verdict)}
		if rj.PeerCIDRs == nil {
			rj.PeerCIDRs = []string{}
		}
		for _, pr := range r.Ports {
			rj.Ports = append(rj.Ports, portRangeJSON{Start: pr.Start, End: pr.End})
		}
		if r.Ruleset != nil {
			rj.Ruleset = &rulesetRefJSON{ID: r.Ruleset.ID.String(), Name: r.Ruleset.Name}
		}
		out.Rules = append(out.Rules, rj)
	}
	return out
}

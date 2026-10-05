// Package ingest receives pre-aggregated flow windows from agents, validates
// them, resolves each record's source address to the peer behind it at that
// moment (a workload with its labels, an address group, or nothing), and
// writes the result through the FlowStore interface (ADR-0009, ADR-0019).
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// ErrInvalidWindow is returned for a report whose window bounds are
// missing or inverted. Individual records that fail validation are
// skipped, not fatal: telemetry is loss-tolerant and one malformed record
// must not cost the rest of the window.
var ErrInvalidWindow = errors.New("ingest: window bounds are missing or inverted")

// maxFieldLength bounds the free-text fields an agent may send.
const maxFieldLength = 256

// MaxGapsPerRequest bounds the evidence gaps one request may carry; past
// it the rest are rejected and counted. An agent holds at most a thousand
// or so and ships them in one request.
const MaxGapsPerRequest = 4096

// Directory is the registry view resolution needs: every workload with
// its current addresses and labels, and every address group. The store
// implements both.
type Directory interface {
	ListWorkloads(ctx context.Context) ([]registry.Workload, error)
	ListAddressGroups(ctx context.Context) ([]policy.AddressGroup, error)
}

// Service ingests reported windows.
type Service struct {
	Directory Directory
	Flows     flowstore.FlowStore
	Log       *slog.Logger
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Result reports one ingested window.
type Result struct {
	// Accepted is how many records were stored.
	Accepted int
	// Rejected is how many records failed validation and were skipped.
	Rejected int
	// Gaps is how many evidence gaps were accepted for storage (a gap
	// already stored is accepted and stored once), and GapsRejected how
	// many failed validation and were skipped.
	Gaps         int
	GapsRejected int
}

// Ingest validates and stores one window reported by workload id. The
// identity comes from the caller, which took it from the connection
// credential; nothing in the request names the reporter. Resolution reads
// the registry once per window, so every record sees the same view.
func (s *Service) Ingest(ctx context.Context, id identity.WorkloadID, req *innerwallv1.ReportFlowsRequest) (Result, error) {
	if req.GetWindowStart() == nil || req.GetWindowEnd() == nil {
		return Result{}, ErrInvalidWindow
	}
	start, end := req.GetWindowStart().AsTime(), req.GetWindowEnd().AsTime()
	if end.Before(start) {
		return Result{}, ErrInvalidWindow
	}
	workloads, err := s.Directory.ListWorkloads(ctx)
	if err != nil {
		return Result{}, err
	}
	groups, err := s.Directory.ListAddressGroups(ctx)
	if err != nil {
		return Result{}, err
	}
	idx := BuildIndex(workloads, groups)
	known := false
	for i := range workloads {
		if workloads[i].ID == id {
			known = true
			break
		}
	}
	if !known {
		return Result{}, registry.ErrWorkloadUnknown
	}

	window := flowstore.Window{WorkloadID: id, Start: start, End: end, Records: make([]flowstore.Record, 0, len(req.GetRecords()))}
	var res Result
	for _, fr := range req.GetRecords() {
		rec, err := convert(fr, start, end)
		if err != nil {
			res.Rejected++
			s.log().Debug("flow record rejected", "workload_id", id, "error", err)
			continue
		}
		rec.Peer = idx.Resolve(rec.SrcAddress)
		window.Records = append(window.Records, rec)
	}
	if res.Rejected > 0 {
		s.log().Warn("flow records rejected", "workload_id", id, "rejected", res.Rejected, "accepted", len(window.Records))
	}
	for i, wg := range req.GetGaps() {
		if i >= MaxGapsPerRequest {
			res.GapsRejected += len(req.GetGaps()) - i
			break
		}
		g, err := convertGap(wg)
		if err != nil {
			res.GapsRejected++
			s.log().Debug("evidence gap rejected", "workload_id", id, "error", err)
			continue
		}
		window.Gaps = append(window.Gaps, g)
	}
	res.Gaps = len(window.Gaps)
	if res.GapsRejected > 0 {
		s.log().Warn("evidence gaps rejected", "workload_id", id, "rejected", res.GapsRejected, "accepted", res.Gaps)
	}
	n, err := s.Flows.WriteWindow(ctx, window)
	if err != nil {
		return res, err
	}
	res.Accepted = n
	return res, nil
}

// convertGap validates one wire gap and shapes it for storage. Unlike a
// record's, a gap's bounds are not the window's: a gap is wherever the
// loss happened.
func convertGap(wg *innerwallv1.EvidenceGap) (flowstore.Gap, error) {
	var g flowstore.Gap
	switch wg.GetKind() {
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_OVERRUN, innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_RESTART,
		innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_BUFFER_OVERFLOW, innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_DUMP_TRUNCATED:
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_UNSPECIFIED:
		return g, errors.New("gap kind unspecified")
	default:
		return g, fmt.Errorf("unknown gap kind %d", wg.GetKind())
	}
	switch wg.GetSource() {
	case innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED, innerwallv1.EvidenceSource_EVIDENCE_SOURCE_CONNTRACK, innerwallv1.EvidenceSource_EVIDENCE_SOURCE_NFLOG:
	default:
		return g, fmt.Errorf("unknown gap source %d", wg.GetSource())
	}
	if wg.GetFrom() == nil || wg.GetTo() == nil {
		return g, errors.New("gap bounds missing")
	}
	from, to := wg.GetFrom().AsTime(), wg.GetTo().AsTime()
	if to.Before(from) {
		return g, errors.New("gap ends before it starts")
	}
	g = flowstore.Gap{Kind: wg.GetKind(), Source: wg.GetSource(), From: from, To: to}
	if wg.Count != nil {
		n := wg.GetCount()
		if n > math.MaxInt64 {
			return flowstore.Gap{}, errors.New("gap count out of range")
		}
		g.Count = &n
	}
	return g, nil
}

// convert validates one wire record and shapes it for storage. Times the
// agent left unset default to the window bounds.
func convert(fr *innerwallv1.FlowRecord, start, end time.Time) (flowstore.Record, error) {
	var rec flowstore.Record
	src, err := netip.ParseAddr(fr.GetSrcAddress())
	if err != nil {
		return rec, fmt.Errorf("source address %q: %w", fr.GetSrcAddress(), err)
	}
	dst, err := netip.ParseAddr(fr.GetDstAddress())
	if err != nil {
		return rec, fmt.Errorf("destination address %q: %w", fr.GetDstAddress(), err)
	}
	if fr.GetDstPort() > 65535 {
		return rec, fmt.Errorf("destination port %d out of range", fr.GetDstPort())
	}
	switch fr.GetProtocol() {
	case innerwallv1.Protocol_PROTOCOL_TCP, innerwallv1.Protocol_PROTOCOL_UDP, innerwallv1.Protocol_PROTOCOL_ICMP:
	case innerwallv1.Protocol_PROTOCOL_UNSPECIFIED:
		return rec, errors.New("protocol unspecified")
	default:
		return rec, fmt.Errorf("unknown protocol %d", fr.GetProtocol())
	}
	// Only inbound observation is produced in v1 (ADR-0010); a record
	// claiming another direction is from an agent this control plane does
	// not understand.
	if fr.GetDirection() != innerwallv1.Direction_DIRECTION_INBOUND {
		return rec, fmt.Errorf("direction %s is not accepted", fr.GetDirection())
	}
	switch fr.GetDecision() {
	case innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED,
		innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK, innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED:
	case innerwallv1.PolicyDecision_POLICY_DECISION_UNSPECIFIED:
		return rec, errors.New("decision unspecified")
	default:
		return rec, fmt.Errorf("unknown decision %d", fr.GetDecision())
	}
	if len(fr.GetMatchedRuleId()) > maxFieldLength || len(fr.GetProcessName()) > maxFieldLength {
		return rec, errors.New("free-text field too long")
	}
	first, last := start, end
	if fr.GetFirstSeen() != nil {
		first = fr.GetFirstSeen().AsTime()
	}
	if fr.GetLastSeen() != nil {
		last = fr.GetLastSeen().AsTime()
	}
	if last.Before(first) {
		return rec, errors.New("last_seen precedes first_seen")
	}
	return flowstore.Record{
		SrcAddress:      src.Unmap(),
		DstAddress:      dst.Unmap(),
		DstPort:         uint16(fr.GetDstPort()), //nolint:gosec // range-checked above
		Protocol:        fr.GetProtocol(),
		Direction:       fr.GetDirection(),
		Decision:        fr.GetDecision(),
		MatchedRuleID:   fr.GetMatchedRuleId(),
		ConnectionCount: fr.GetConnectionCount(),
		ByteCount:       fr.GetByteCount(),
		FirstSeen:       first,
		LastSeen:        last,
		ProcessName:     fr.GetProcessName(),
	}, nil
}

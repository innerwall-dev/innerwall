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

// SilenceWindows is how many flow windows may pass between consecutive
// reports of a workload before the silence between them is an evidence
// gap (ADR-0019 as amended). The reporter ships one window per window
// length, so two allow one late report — the same allowance after which
// the read model stops calling the workload's telemetry live
// (readmodel.TelemetryLiveWindows; a test keeps the two equal). Below it,
// an agent restarted quickly enough to miss no whole window records no
// gap.
const SilenceWindows = 2

// Directory is the registry view resolution needs: every workload with
// its current addresses and labels, and every address group. The store
// implements both.
type Directory interface {
	ListWorkloads(ctx context.Context) ([]registry.Workload, error)
	ListAddressGroups(ctx context.Context) ([]policy.AddressGroup, error)
}

// ClockRecorder records what a flow report said about its agent's clock
// on the workload: a skewed reading's offset and instant, or that the
// latest reading was within tolerance. The store implements it.
type ClockRecorder interface {
	RecordClockReading(ctx context.Context, id identity.WorkloadID, offset time.Duration, skewed bool, receivedAt time.Time) error
}

// ReportRecorder records the instant the control plane received a flow
// report from a workload. A report proves the agent is running whatever
// the state of its sync stream, which travels on a connection of its own.
// It returns the receipt this one replaced (nil for the first ever), the
// agent process the workload's latest heartbeat proved running (nil when
// none is recorded), and whether it advanced at all: a stale receipt,
// handled out of order, replaces nothing. The implementation must hand the
// replaced instant to exactly one of two concurrent reports, so a silence
// is derived once, and read the process under the same lock; the store
// implements it with one locking statement.
type ReportRecorder interface {
	RecordFlowReport(ctx context.Context, id identity.WorkloadID, receivedAt time.Time) (prev *time.Time, proc *registry.AgentProcess, advanced bool, err error)
}

// Service ingests reported windows.
type Service struct {
	Directory Directory
	Flows     flowstore.FlowStore
	// Clock records each report's clock reading on its workload; nil
	// records nothing, and a skewed report is still logged.
	Clock ClockRecorder
	// Reports records the receipt of every report on its workload; nil
	// records nothing.
	Reports ReportRecorder
	// FlowWindow is the flow aggregation window the control plane hands
	// agents in SyncConfig; the clock-skew tolerance is ClockSkewWindows
	// of it. Zero skips the clock check.
	FlowWindow time.Duration
	Log        *slog.Logger
	// Now is the clock receipt is read from; time.Now if nil.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
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
	// Derived is how many gaps this report made the control plane derive
	// and store itself: one when it ended a silence longer than
	// SilenceWindows flow windows, zero otherwise (ADR-0019 as amended).
	Derived int
}

// Ingest validates and stores one window reported by workload id. The
// identity comes from the caller, which took it from the connection
// credential; nothing in the request names the reporter. Resolution reads
// the registry once per window, so every record sees the same view. The
// report's clock reading is recorded on the workload and a skewed one is
// logged, but skew rejects nothing: the window is stored as reported.
//
// The clock is read before the window's bounds are validated. The send
// instant is independent of the bounds, and a clock stepped while a
// window was open is what inverts them, so a report refused for its
// bounds still records the skew that most likely produced it.
func (s *Service) Ingest(ctx context.Context, id identity.WorkloadID, req *innerwallv1.ReportFlowsRequest) (Result, error) {
	receivedAt := s.now()
	workloads, err := s.Directory.ListWorkloads(ctx)
	if err != nil {
		return Result{}, err
	}
	var reporter *registry.Workload
	for i := range workloads {
		if workloads[i].ID == id {
			reporter = &workloads[i]
			break
		}
	}
	if reporter == nil {
		return Result{}, registry.ErrWorkloadUnknown
	}
	if reporter.Retired() {
		// The interceptor refuses a retired identity; this is a report
		// admitted just before the retirement committed.
		return Result{}, &registry.RetiredError{ID: id, RetiredAt: *reporter.RetiredAt}
	}
	var res Result
	prev, proc, advanced := s.recordReport(ctx, id, receivedAt)
	if advanced {
		res.Derived = s.recordSilence(ctx, id, prev, proc, req, receivedAt)
	}
	s.checkClock(ctx, reporter, req, receivedAt)

	if req.GetWindowStart() == nil || req.GetWindowEnd() == nil {
		return res, ErrInvalidWindow
	}
	start, end := req.GetWindowStart().AsTime(), req.GetWindowEnd().AsTime()
	if end.Before(start) {
		return res, ErrInvalidWindow
	}
	groups, err := s.Directory.ListAddressGroups(ctx)
	if err != nil {
		return res, err
	}
	idx := BuildIndex(workloads, groups)

	window := flowstore.Window{WorkloadID: id, Start: start, End: end, Records: make([]flowstore.Record, 0, len(req.GetRecords()))}
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

// recordReport stamps the receipt of a report on its workload, before the
// window is validated: a report refused for its bounds still came from a
// running agent. A failed record is logged and costs the window nothing.
// It returns the receipt this one replaced, the agent process last proved
// running, and whether it advanced, for the silence derivation.
func (s *Service) recordReport(ctx context.Context, id identity.WorkloadID, receivedAt time.Time) (prev *time.Time, proc *registry.AgentProcess, advanced bool) {
	if s.Reports == nil {
		return nil, nil, false
	}
	prev, proc, advanced, err := s.Reports.RecordFlowReport(ctx, id, receivedAt)
	if err != nil {
		s.log().Warn("recording flow report receipt", "workload_id", id, "error", err)
		return nil, nil, false
	}
	return prev, proc, advanced
}

// recordSilence derives and stores the agent-silence evidence gap a report
// reveals, if any, and returns how many gaps it stored (ADR-0019 as
// amended; issue #114). An agent cannot report the interval in which its
// own process was down — its gaps cover only losses it observed, and
// ADR-0011 gives it no on-disk instant to reopen at start — but the
// control plane saw the silence: no report arrived. A failed write is
// logged and costs the window nothing; the receipt has already advanced,
// so the gap is not derived again, and the loss it bounded stays unstated
// — the write shares the report's database, so a write that fails here
// would have failed the whole window.
func (s *Service) recordSilence(ctx context.Context, id identity.WorkloadID, prev *time.Time, proc *registry.AgentProcess, req *innerwallv1.ReportFlowsRequest, receivedAt time.Time) int {
	gap, ok := deriveSilence(prev, proc, req, receivedAt, s.FlowWindow)
	if !ok {
		return 0
	}
	if err := s.Flows.WriteGaps(ctx, id, []flowstore.Gap{gap}); err != nil {
		s.log().Warn("recording derived silence gap", "workload_id", id, "gap_from", gap.From, "gap_to", gap.To, "error", err)
		return 0
	}
	s.log().Info("agent silence recorded as evidence gap", "workload_id", id, "gap_from", gap.From, "gap_to", gap.To, "silence", receivedAt.Sub(*prev).Round(time.Second).String())
	return 1
}

// deriveSilence decides whether the silence a report ended is an evidence
// gap, and bounds it. The bounds are receipt instants on the control
// plane's clock, refined by the arriving window:
//
//   - No gap while the silence is within SilenceWindows flow windows: the
//     reporter ships one window per window length, so a quick agent
//     restart that loses no whole window derives nothing. No gap either
//     with no window length to judge by, before the first report ever, or
//     when the recorder did not advance (a stale report says nothing new).
//   - No gap when one agent process ran through the whole silence: the
//     agent ships only windows that hold something, so a running agent on
//     a workload that receives little goes many windows between reports
//     having lost nothing (issue #144). The latest heartbeat proves it when
//     the process it came from started at or before the previous receipt
//     and still ran within one flow window of this one: the window this
//     report carries was open for a window length before it was sent, so
//     the process that sent it was already running then, and two of one
//     agent's processes never overlap — the process that ran at the
//     previous report is the one reporting now. Whatever a running process
//     loses it reports itself (a source restart, a full buffer, a removed
//     table), so the silence holds no loss of this kind. A heartbeat from
//     before a restart fails the test however late it arrived, since the
//     restarted process opened its first window after the old one was
//     gone; a sync stream that is down leaves the latest heartbeat too old,
//     and the derivation stands as below.
//   - The gap opens at the previous report's receipt: the last instant
//     evidence is known to have reached the control plane. Neither the
//     sync stream's lifecycle nor the old process's last heartbeat moves
//     it later — an agent whose telemetry
//     connection failed buffers closed windows in memory and loses them if
//     the process then dies, so instants the stream was alive past the
//     last report are not instants whose flow evidence survived.
//   - The gap closes where evidence resumes: at the arriving window's
//     start when that lies within the silence (an agent restarted at t
//     opens its first window at t), and at this report's receipt when the
//     report carries no usable window start. A window that reaches back to
//     or before the previous receipt is buffered evidence delivered late —
//     the silence was the connection's, the evidence survived, and there
//     is no gap; what such a buffer dropped, the agent itself reports
//     (buffer_overflow), so the one loss is not stored twice.
func deriveSilence(prev *time.Time, proc *registry.AgentProcess, req *innerwallv1.ReportFlowsRequest, receivedAt time.Time, window time.Duration) (flowstore.Gap, bool) {
	if !silent(prev, proc, receivedAt, window) {
		return flowstore.Gap{}, false
	}
	to := receivedAt
	if ws := req.GetWindowStart(); ws != nil {
		start := ws.AsTime()
		if !start.After(*prev) {
			return flowstore.Gap{}, false
		}
		if start.Before(receivedAt) {
			to = start
		}
	}
	return flowstore.Gap{
		Kind:   innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT,
		Source: innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED,
		From:   *prev,
		To:     to,
	}, true
}

// SilenceAtRetirement is the agent-silence gap a retirement at instant at
// closes, if one is open (ADR-0023; issue #126): the gap deriveSilence
// would have written had a report arrived at that instant, by the same
// rules. A silence since the last receipt prev longer than SilenceWindows
// flow windows, which no heartbeat shows one agent process running
// through, is one gap from prev to at. A workload that never reported, or
// whose agent was demonstrably running and quiet, has none. No report can
// follow a retirement, so there is no arriving window to refine the bound
// by and the gap is never derived twice.
func SilenceAtRetirement(prev *time.Time, proc *registry.AgentProcess, at time.Time, window time.Duration) (flowstore.Gap, bool) {
	if !silent(prev, proc, at, window) {
		return flowstore.Gap{}, false
	}
	return flowstore.Gap{
		Kind:   innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT,
		Source: innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED,
		From:   *prev,
		To:     at,
	}, true
}

// silent says the interval from the last receipt prev to instant at is a
// silence the evidence lost: there was a receipt, there is a window length
// to judge by, the interval exceeds SilenceWindows of them, and no agent
// process ran through it.
func silent(prev *time.Time, proc *registry.AgentProcess, at time.Time, window time.Duration) bool {
	if window <= 0 || prev == nil {
		return false
	}
	if at.Sub(*prev) <= SilenceWindows*window {
		return false
	}
	return !ranThrough(proc, *prev, at, window)
}

// ranThrough says the agent process proc ran from at or before the
// previous receipt until within one flow window of this one, so it is the
// process that sent this report and no restart lies in the silence.
func ranThrough(proc *registry.AgentProcess, prev, receivedAt time.Time, window time.Duration) bool {
	return proc != nil && !proc.StartedAt.After(prev) && !proc.HeartbeatAt.Before(receivedAt.Add(-window))
}

// checkClock reads the report's clock, logs a skewed reading naming the
// workload and the offset, and records the reading on the workload. A
// failed record is logged and costs the window nothing.
func (s *Service) checkClock(ctx context.Context, w *registry.Workload, req *innerwallv1.ReportFlowsRequest, receivedAt time.Time) {
	if s.FlowWindow <= 0 {
		return
	}
	// A report with neither a send instant nor a window end carries no
	// reading of the agent's clock.
	if req.GetSentAt() == nil && req.GetWindowEnd() == nil {
		return
	}
	r := ReadClock(req, receivedAt, s.FlowWindow)
	if r.Skewed {
		s.log().Warn("agent clock skewed: flow windows are stamped with the agent's clock",
			"workload_id", w.ID, "hostname", w.Hostname,
			"offset", r.Offset.Round(time.Second).String(), "offset_seconds", int64(r.Offset.Round(time.Second)/time.Second),
			"measured_by", r.MeasuredBy, "tolerance", (ClockSkewWindows * s.FlowWindow).String(),
			"window_end", req.GetWindowEnd().AsTime())
	}
	if s.Clock == nil {
		return
	}
	if err := s.Clock.RecordClockReading(ctx, w.ID, r.Offset, r.Skewed, receivedAt); err != nil {
		s.log().Warn("recording clock reading", "workload_id", w.ID, "error", err)
	}
}

// convertGap validates one wire gap and shapes it for storage. Unlike a
// record's, a gap's bounds are not the window's: a gap is wherever the
// loss happened.
func convertGap(wg *innerwallv1.EvidenceGap) (flowstore.Gap, error) {
	var g flowstore.Gap
	switch wg.GetKind() {
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_OVERRUN, innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_RESTART,
		innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_BUFFER_OVERFLOW, innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_DUMP_TRUNCATED,
		innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_WINDOW_OVERFLOW, innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_WINDOW_REJECTED,
		innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_TABLE_REMOVED:
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_UNSPECIFIED:
		return g, errors.New("gap kind unspecified")
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT:
		// The one kind the control plane derives itself: an agent cannot
		// observe its own absence, so a reported claim of it is refused.
		return g, errors.New("gap kind agent_silent is derived by the control plane, never reported")
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

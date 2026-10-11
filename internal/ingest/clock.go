package ingest

import (
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// ClockSkewWindows is the clock-skew tolerance in flow aggregation
// windows: a report whose agent clock differs from the control plane's by
// more than this many windows is skewed. Two windows is generous for
// transport (a report is stamped immediately before it is sent and its
// stream is bounded at thirty seconds) and tight enough for real drift,
// which runs to minutes or hours: a VM's clock after its host slept, or a
// host whose time sync has failed. A skewed agent stamps every window it
// reports with the wrong clock, so its flows land at the wrong times.
// It is a constant, not configuration: the window length is the knob.
const ClockSkewWindows = 2

// Measures name how a clock reading was taken, for the log.
const (
	// MeasuredBySentAt is a reading from the agent's clock at send.
	MeasuredBySentAt = "sent_at"
	// MeasuredByWindowEnd is a reading from the window's end, for an
	// agent that predates sent_at.
	MeasuredByWindowEnd = "window_end"
)

// ClockReading is what one flow report says about its agent's clock.
type ClockReading struct {
	// Offset is the agent's clock minus the control plane's: negative
	// when the agent is behind.
	Offset time.Duration
	// MeasuredBy is MeasuredBySentAt or MeasuredByWindowEnd.
	MeasuredBy string
	// Skewed says the offset is beyond the tolerance.
	Skewed bool
	// ReceivedAt is when the control plane received the report.
	ReceivedAt time.Time
}

// ReadClock compares one report with the instant it was received, under
// a tolerance of ClockSkewWindows flow windows.
//
// A report that carries sent_at is read from it: the agent's clock at
// send against the control plane's at receipt, skewed in either
// direction. A report without it, from an agent that predates the field,
// is read from its window's end, and is skewed only when that end is
// ahead of receipt by more than the tolerance. A window can arrive any
// time after it ends, because the agent holds closed windows in its
// buffer while the control plane is unreachable and delivers them late by
// design (ADR-0019), so a late window says nothing about the clock; a
// window cannot legitimately arrive before it ends, so an early one does.
func ReadClock(req *innerwallv1.ReportFlowsRequest, receivedAt time.Time, window time.Duration) ClockReading {
	tolerance := ClockSkewWindows * window
	if sent := req.GetSentAt(); sent != nil {
		offset := sent.AsTime().Sub(receivedAt)
		return ClockReading{Offset: offset, MeasuredBy: MeasuredBySentAt, Skewed: offset > tolerance || offset < -tolerance, ReceivedAt: receivedAt}
	}
	offset := req.GetWindowEnd().AsTime().Sub(receivedAt)
	return ClockReading{Offset: offset, MeasuredBy: MeasuredByWindowEnd, Skewed: offset > tolerance, ReceivedAt: receivedAt}
}

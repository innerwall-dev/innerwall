// Package health is reserved for a whole-process resource budget, which
// is deferred (ADR-0011 as amended); it holds no code. What it once
// described lives elsewhere: heartbeats ride the sync stream (agent/sync),
// the telemetry the agent holds is bounded where it is held, the open
// window in keys and the closed-window buffer in records, with every drop
// recorded as an evidence gap (agent/collect), and the local kill switch
// is `innerwall-agent down` (agent/enforce/nft.Teardown).
package health

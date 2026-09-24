// Package fleet is the operator's write domain over the registry and the
// renderer: editing a workload's labels, changing the enforcement mode of
// a set of workloads, previewing what a selector resolves to, and
// rendering a hypothetical policy set without persisting it. The command
// line and the operator surface call these functions and nothing else,
// which is what keeps the two transports from drifting (ADR-0007 as
// amended, ADR-0021).
//
// A mode change is a fan-out mutation: it resolves its selector through
// the renderer's own scope match, records the intent as the set it
// resolved to, flips the desired mode of exactly that set, and renders,
// all in one transaction under the render lock (ADR-0018). What it
// returns is an acknowledgment of the recorded intent, never a job:
// convergence is observed on each workload's applied version against its
// latest (ADR-0007 as amended). A dry run and a preview are pure reads of
// persisted state: no lock is held across them and nothing is written.
package fleet

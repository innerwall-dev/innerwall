package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// The flows commands are the read shapes the operator console will issue
// (ADR-0019): a workload's windows over a time range, a decision-filtered
// rollup by peer and service over a label scope, and a workload's
// cumulative totals since first seen. They read through the FlowStore and
// nothing else (ADR-0009).

func runFlows(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall flows list|rollup|totals [flags]")
	}
	switch args[0] {
	case "list":
		return runFlowsList(ctx, args[1:])
	case "rollup":
		return runFlowsRollup(ctx, args[1:])
	case "totals":
		return runFlowsTotals(ctx, args[1:])
	default:
		return usageError("unknown flows command %q (list|rollup|totals)", args[0])
	}
}

// parseDecision accepts the decision names without their prefix; empty
// means every decision.
func parseDecision(s string) (innerwallv1.PolicyDecision, error) {
	if s == "" {
		return innerwallv1.PolicyDecision_POLICY_DECISION_UNSPECIFIED, nil
	}
	v, ok := innerwallv1.PolicyDecision_value["POLICY_DECISION_"+strings.ToUpper(strings.ReplaceAll(s, "-", "_"))]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown decision %q (observed|allowed|would_block|blocked)", s)
	}
	return innerwallv1.PolicyDecision(v), nil
}

func decisionName(d innerwallv1.PolicyDecision) string {
	return strings.ToLower(strings.TrimPrefix(d.String(), "POLICY_DECISION_"))
}

// peerNames maps workload and address-group ids to display names.
type peerNames struct {
	workloads map[string]registry.Workload
	groups    map[string]string
}

func loadPeerNames(ctx context.Context, st interface {
	ListWorkloads(context.Context) ([]registry.Workload, error)
	ListAddressGroups(context.Context) ([]policy.AddressGroup, error)
}) (*peerNames, error) {
	workloads, err := st.ListWorkloads(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := st.ListAddressGroups(ctx)
	if err != nil {
		return nil, err
	}
	n := &peerNames{workloads: map[string]registry.Workload{}, groups: map[string]string{}}
	for _, w := range workloads {
		n.workloads[w.ID.String()] = w
	}
	for _, g := range groups {
		n.groups[g.ID.String()] = g.Name
	}
	return n, nil
}

// describe renders a resolved peer: the workload's hostname and the labels
// captured at ingest, the address group's name, or the bare address.
func (n *peerNames) describe(p flowstore.Peer) string {
	switch p.Kind {
	case flowstore.PeerWorkload:
		name := p.Key
		if w, ok := n.workloads[p.Key]; ok && w.Hostname != "" {
			name = w.Hostname
		}
		if len(p.Labels) > 0 {
			return name + " [" + labelMapString(p.Labels) + "]"
		}
		return name
	case flowstore.PeerAddressGroup:
		if name, ok := n.groups[p.Key]; ok {
			return "group:" + name
		}
		return "group:" + p.Key
	case flowstore.PeerUnknown:
		return p.Key
	default:
		return p.Key
	}
}

func labelMapString(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func serviceName(port uint16, p innerwallv1.Protocol) string {
	if p == innerwallv1.Protocol_PROTOCOL_ICMP {
		return policy.ProtocolName(p)
	}
	return fmt.Sprintf("%s/%d", policy.ProtocolName(p), port)
}

// timeRange resolves --since/--until: durations relative to now, or
// RFC 3339 instants.
func timeRange(since, until string, now time.Time) (time.Time, time.Time, error) {
	parse := func(s string, fallback time.Time) (time.Time, error) {
		if s == "" {
			return fallback, nil
		}
		if d, err := time.ParseDuration(s); err == nil {
			return now.Add(-d), nil
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("%q is neither a duration nor an RFC 3339 time", s)
		}
		return t, nil
	}
	from, err := parse(since, now.Add(-24*time.Hour))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parse(until, now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("--until must be after --since")
	}
	return from, to, nil
}

func runFlowsList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall flows list", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	since := fs.String("since", "24h", "start of the range: a duration before now or an RFC 3339 time")
	until := fs.String("until", "", "end of the range: a duration before now or an RFC 3339 time (default now)")
	decision := fs.String("decision", "", "only this decision: observed|allowed|would_block|blocked (default all)")
	limit := fs.Int("limit", 200, "maximum rows, newest window first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall flows list <workload-id> [--since 24h] [--until ...] [--decision ...] [--limit 200]")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	dec, err := parseDecision(*decision)
	if err != nil {
		return err
	}
	from, to, err := timeRange(*since, *until, time.Now())
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, err := st.LookupWorkload(ctx, id); err != nil {
		return err
	}
	names, err := loadPeerNames(ctx, st)
	if err != nil {
		return err
	}
	rows, err := st.Flows().ListWindows(ctx, flowstore.WindowQuery{WorkloadID: id, Since: from, Until: to, Decision: dec, Limit: *limit})
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "WINDOW\tPEER\tSOURCE\tSERVICE\tDECISION\tRULE\tCONNS\tBYTES")
	for i := range rows {
		r := &rows[i]
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
			r.WindowStart.UTC().Format(time.RFC3339), names.describe(r.Peer), r.SrcAddress, serviceName(r.DstPort, r.Protocol),
			decisionName(r.Decision), r.MatchedRuleID, r.ConnectionCount, r.ByteCount)
	}
	return w.Flush()
}

func runFlowsRollup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall flows rollup", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	since := fs.String("since", "24h", "start of the range: a duration before now or an RFC 3339 time")
	until := fs.String("until", "", "end of the range: a duration before now or an RFC 3339 time (default now)")
	decision := fs.String("decision", "", "only this decision: observed|allowed|would_block|blocked (default all)")
	var labels labelFlags
	fs.Var(&labels, "label", "label key=value selecting the workloads in scope (repeatable, ANDed; none selects every workload)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageError("usage: innerwall flows rollup [--label key=value ...] [--decision would_block] [--since 24h]")
	}
	dec, err := parseDecision(*decision)
	if err != nil {
		return err
	}
	from, to, err := timeRange(*since, *until, time.Now())
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	names, err := loadPeerNames(ctx, st)
	if err != nil {
		return err
	}
	// The scope is resolved here, against current labels: a rollup asks
	// what the workloads that are in scope now have seen.
	selector := policy.Selector{}
	for _, l := range labels {
		selector[l.Key] = append(selector[l.Key], l.Value)
	}
	var ids []identity.WorkloadID
	for _, w := range names.workloads {
		if len(selector) == 0 || selector.Matches(w.LabelMap()) {
			ids = append(ids, w.ID)
		}
	}
	rows, err := st.Flows().Rollup(ctx, flowstore.RollupQuery{WorkloadIDs: ids, Since: from, Until: to, Decision: dec})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d workloads in scope, %s to %s\n", len(ids), from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PEER\tSERVICE\tDECISION\tWORKLOADS\tCONNS\tBYTES\tFIRST SEEN\tLAST SEEN")
	for i := range rows {
		r := &rows[i]
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			names.describe(r.Peer), serviceName(r.DstPort, r.Protocol), decisionName(r.Decision), r.Workloads,
			r.ConnectionCount, r.ByteCount, r.FirstSeen.UTC().Format(time.RFC3339), r.LastSeen.UTC().Format(time.RFC3339))
	}
	return w.Flush()
}

func runFlowsTotals(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall flows totals", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	decision := fs.String("decision", "", "only this decision: observed|allowed|would_block|blocked (default all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall flows totals <workload-id> [--decision ...]")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	dec, err := parseDecision(*decision)
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, err := st.LookupWorkload(ctx, id); err != nil {
		return err
	}
	names, err := loadPeerNames(ctx, st)
	if err != nil {
		return err
	}
	rows, err := st.Flows().ListTotals(ctx, id, dec)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PEER\tSERVICE\tDECISION\tRULE\tCONNS\tBYTES\tWINDOWS\tFIRST SEEN\tLAST SEEN")
	for i := range rows {
		r := &rows[i]
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			names.describe(r.Peer), serviceName(r.DstPort, r.Protocol), decisionName(r.Decision), r.MatchedRuleID,
			r.ConnectionCount, r.ByteCount, r.WindowCount, r.FirstSeen.UTC().Format(time.RFC3339), r.LastSeen.UTC().Format(time.RFC3339))
	}
	return w.Flush()
}

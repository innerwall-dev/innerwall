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
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store"
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
	return readmodel.ParseVerdict(s)
}

func decisionName(d innerwallv1.PolicyDecision) string {
	return readmodel.VerdictName(d)
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
	groupBy := fs.String("group-by", "", "the operator surface's grouped rollup instead of the peer-and-service one: "+flowstore.GroupByNames())
	workload := fs.String("workload", "", "with --group-by: only this workload's flows")
	service := fs.String("service", "", "with --group-by: only this service, <protocol>/<port> or icmp")
	order := fs.String("order", "connections", "with --group-by: connections | recent")
	limit := fs.Int("limit", flowstore.DefaultGroupLimit, "with --group-by: the most groups shown; the rest are counted in the totals")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageError("usage: innerwall flows rollup [--label key=value ...] [--decision would_block] [--since 24h] [--group-by rule|rule,peer|src,dst|dst,service]")
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
	if *groupBy != "" {
		return runFlowsGroupedRollup(ctx, st, groupedRollupArgs{groupBy: *groupBy, labels: labels, decision: dec, from: from, to: to, workload: *workload, service: *service, order: *order, limit: *limit})
	}
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

type groupedRollupArgs struct {
	groupBy  string
	labels   labelFlags
	decision innerwallv1.PolicyDecision
	from, to time.Time
	workload string
	service  string
	order    string
	limit    int
}

// runFlowsGroupedRollup issues the read model's grouped rollup, the same
// function the operator surface serves at /api/v1/flows/rollup.
func runFlowsGroupedRollup(ctx context.Context, st *store.Store, a groupedRollupArgs) error {
	req := readmodel.RollupRequest{From: a.from, To: a.to, Verdict: a.decision, Selector: policy.Selector{}, Order: flowstore.GroupOrder(a.order), Limit: a.limit}
	var err error
	if req.GroupBy, err = flowstore.ParseGroupBy(a.groupBy); err != nil {
		return err
	}
	for _, l := range a.labels {
		req.Selector[l.Key] = append(req.Selector[l.Key], l.Value)
	}
	if a.workload != "" {
		id, err := identity.ParseWorkloadID(a.workload)
		if err != nil {
			return err
		}
		req.Workload = &id
	}
	if a.service != "" {
		svc, err := readmodel.ParseService(a.service)
		if err != nil {
			return err
		}
		req.Service = &svc
	}
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	res, err := reads.Rollup(ctx, req)
	if err != nil {
		return err
	}
	covered := "no windows"
	if res.EffectiveFrom != nil {
		covered = res.EffectiveFrom.UTC().Format(time.RFC3339) + " to " + res.EffectiveTo.UTC().Format(time.RFC3339)
	}
	fmt.Fprintf(os.Stderr, "%s to %s requested, %s covered; %d groups, %d records, %d connections", res.Range.From.Format(time.RFC3339), res.Range.To.Format(time.RFC3339), covered, res.GroupCount, res.Totals.FlowCount, res.Totals.ConnectionCount)
	if res.Truncated {
		fmt.Fprintf(os.Stderr, "; showing the top %d", len(res.Groups))
	}
	fmt.Fprintln(os.Stderr)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, strings.ToUpper(strings.ReplaceAll(a.groupBy, ",", "\t"))+"\tRECORDS\tCONNS\tBYTES\tFIRST SEEN\tLAST SEEN")
	for i := range res.Groups {
		g := &res.Groups[i]
		keys := make([]string, 0, 2)
		for _, k := range req.GroupBy.Keys() {
			switch k {
			case "rule":
				keys = append(keys, ruleKeyString(g.Keys.Rule))
			case "peer":
				keys = append(keys, peerRefString(g.Keys.Peer))
			case "src":
				keys = append(keys, peerRefString(g.Keys.Src))
			case "dst":
				keys = append(keys, workloadRefString(g.Keys.Dst))
			case "service":
				keys = append(keys, g.Keys.Service.String())
			}
		}
		_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\t%s\n", strings.Join(keys, "\t"), g.FlowCount, g.ConnectionCount, g.ByteCount, g.FirstSeen.UTC().Format(time.RFC3339), g.LastSeen.UTC().Format(time.RFC3339))
	}
	return w.Flush()
}

func ruleKeyString(r *readmodel.RuleRef) string {
	if r == nil {
		return "(no rule)"
	}
	return r.ID
}

func peerRefString(p *readmodel.PeerRef) string {
	if p == nil {
		return ""
	}
	name := p.Key
	if p.Name != "" {
		name = p.Name
	}
	switch p.Kind {
	case flowstore.PeerWorkload:
		if len(p.Labels) > 0 {
			return name + " [" + labelMapString(p.Labels) + "]"
		}
		return name
	case flowstore.PeerAddressGroup:
		return "group:" + name
	case flowstore.PeerUnknown:
		return p.Key
	default:
		return p.Key
	}
}

func workloadRefString(w *readmodel.WorkloadRef) string {
	if w == nil {
		return ""
	}
	if w.Hostname != "" {
		return w.Hostname
	}
	return w.ID.String()
}

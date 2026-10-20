package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/innerwall-dev/innerwall/internal/cli"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

func runWorkload(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall workload list|counts|status|set-labels|set-mode|resend-snapshot|retire [flags]")
	}
	switch args[0] {
	case "list":
		return runWorkloadList(ctx, args[1:])
	case "counts":
		return runWorkloadCounts(ctx, args[1:])
	case "status":
		return runWorkloadStatus(ctx, args[1:])
	case "set-labels":
		return runWorkloadSetLabels(ctx, args[1:])
	case "set-mode":
		return runWorkloadSetMode(ctx, args[1:])
	case "resend-snapshot":
		return runWorkloadResendSnapshot(ctx, args[1:])
	case "retire":
		return runWorkloadRetire(ctx, args[1:])
	default:
		return usageError("unknown workload command %q (list|counts|status|set-labels|set-mode|resend-snapshot|retire)", args[0])
	}
}

func syncStateName(s registry.Workload) string {
	return readmodel.SyncStateName(s.SyncState)
}

func labelString(labels []registry.Label) string {
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		parts = append(parts, l.Key+"="+l.Value)
	}
	return strings.Join(parts, ",")
}

func ago(t *time.Time, now time.Time) string {
	if t == nil {
		return "never"
	}
	return now.Sub(*t).Truncate(time.Second).String() + " ago"
}

func runWorkloadList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload list", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := cli.Parse(fs, args, 0); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	workloads, err := st.ListWorkloads(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tHOSTNAME\tLABELS\tMODE\tSYNC\tAPPLIED/LATEST\tLAST SEEN")
	for i := range workloads {
		wl := &workloads[i]
		latest := "-"
		if p, err := st.GetWorkloadPolicy(ctx, wl.ID); err == nil && p != nil {
			latest = fmt.Sprint(p.GetVersion())
		}
		sync := syncStateName(*wl)
		if wl.Retired() {
			// Retired is a state of its own, not a sync state; the
			// stored one is only the last the stream recorded (ADR-0023).
			sync = "RETIRED " + wl.RetiredAt.UTC().Format(time.RFC3339)
		} else if wl.OwnedTable == innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT {
			// The version stands acknowledged; the table it was installed
			// in is gone (issue #64).
			sync += ", ENFORCEMENT REMOVED"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d/%s\t%s\n", wl.ID, wl.Hostname, labelString(wl.Labels), policy.ModeName(wl.Mode), sync, wl.AppliedVersion, latest, ago(wl.LastSeenAt, now))
	}
	return w.Flush()
}

// runWorkloadCounts prints the active fleet's size in total, removed, and
// in each sync state, and the retired workloads, through the read the
// operator surface's GET /workloads/counts serves. A removed workload is
// counted in removed alone, so the lines above the total sum to it; a
// retired workload is outside it and counted in retired alone.
func runWorkloadCounts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload counts", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := cli.Parse(fs, args, 0); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	counts, err := reads.CountWorkloads(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "removed\t%d\n", counts.Removed)
	for _, state := range []innerwallv1.SyncState{
		innerwallv1.SyncState_SYNC_STATE_DEGRADED,
		innerwallv1.SyncState_SYNC_STATE_OFFLINE,
		innerwallv1.SyncState_SYNC_STATE_PENDING,
		innerwallv1.SyncState_SYNC_STATE_SYNCED,
	} {
		_, _ = fmt.Fprintf(w, "%s\t%d\n", readmodel.SyncStateName(state), counts.BySync[state])
	}
	_, _ = fmt.Fprintf(w, "total\t%d\n", counts.Total)
	_, _ = fmt.Fprintf(w, "retired\t%d\n", counts.Retired)
	return w.Flush()
}

func runWorkloadStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload status", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := cli.Parse(fs, args, 1); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall workload status <workload-id>")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	wl, err := st.LookupWorkload(ctx, id)
	if err != nil {
		return err
	}
	latest := uint64(0)
	if p, err := st.GetWorkloadPolicy(ctx, id); err != nil {
		return err
	} else if p != nil {
		latest = p.GetVersion()
	}
	services, err := st.ListListeningServices(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now()
	fmt.Printf("workload        %s\n", wl.ID)
	fmt.Printf("hostname        %s\n", wl.Hostname)
	fmt.Printf("labels          %s\n", labelString(wl.Labels))
	fmt.Printf("mode            %s\n", policy.ModeName(wl.Mode))
	if wl.Retired() {
		fmt.Printf("lifecycle       RETIRED at %s: its credential is refused and it renders nothing; the record stays\n", wl.RetiredAt.UTC().Format(time.RFC3339))
	} else {
		fmt.Printf("lifecycle       active\n")
	}
	fmt.Printf("sync state      %s\n", syncStateName(*wl))
	switch wl.OwnedTable {
	case innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT:
		fmt.Printf("owned table     ABSENT since %s: enforcement removed on the host, nothing is enforced or observed\n", ago(wl.OwnedTableAbsentSince, now))
	case innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT, innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED:
		fmt.Printf("owned table     %s\n", readmodel.OwnedTableStateName(wl.OwnedTable))
	default:
		fmt.Printf("owned table     %s\n", readmodel.OwnedTableStateName(wl.OwnedTable))
	}
	if wl.SyncError != "" {
		fmt.Printf("sync error      %s\n", wl.SyncError)
	}
	fmt.Printf("applied version %d\n", wl.AppliedVersion)
	fmt.Printf("latest version  %d\n", latest)
	fmt.Printf("last seen       %s (control channel)\n", ago(wl.LastSeenAt, now))
	fmt.Printf("last flow report %s\n", ago(wl.LastFlowReportAt, now))
	fmt.Printf("last snapshot   %s\n", ago(wl.LastSnapshotSentAt, now))
	fmt.Printf("last ack        %s\n", ago(wl.LastAckedAt, now))
	fmt.Printf("apply failed    %s\n", ago(wl.LastApplyFailedAt, now))
	fmt.Printf("enrolled        %s\n", wl.EnrolledAt.UTC().Format(time.RFC3339))
	fmt.Printf("credential      expires %s\n", wl.CredentialExpiresAt.UTC().Format(time.RFC3339))
	if l := wl.LateRenewal; l != nil {
		fmt.Printf("renewed late    %s, %s after the credential expired\n", ago(&l.RenewedAt, now), l.RenewedAt.Sub(l.ExpiredAt).Round(time.Second))
	}
	fmt.Printf("agent           %s\n", wl.Agent.Version)
	fmt.Printf("dropped flows   %d\n", wl.DroppedFlowRecords)
	fmt.Printf("overruns        %d\n", wl.SourceOverruns)
	if c := wl.ClockSkew; c != nil {
		state := "current"
		if !c.Current {
			state = "since back within tolerance"
		}
		fmt.Printf("clock skew      %s, agent minus control plane, last skewed report %s; %s\n", c.Offset.Round(time.Second), ago(&c.ObservedAt, now), state)
	}
	if wl.CredentialRenewalError != "" {
		fmt.Printf("renewal error   %s\n", wl.CredentialRenewalError)
	}
	addrs := make([]string, 0, len(wl.Addresses))
	for _, a := range wl.Addresses {
		addrs = append(addrs, a.String())
	}
	fmt.Printf("addresses       %s\n", strings.Join(addrs, " "))
	if wl.Facts != nil && wl.Facts.GetOs() != nil {
		os := wl.Facts.GetOs()
		fmt.Printf("os              %s %s %s (%s, %s)\n", os.GetFamily(), os.GetName(), os.GetVersion(), os.GetKernelVersion(), os.GetArchitecture())
	}
	if len(services) > 0 {
		fmt.Println("listening")
		for _, s := range services {
			fmt.Printf("  %s/%d  %s %s\n", policy.ProtocolName(s.Protocol), s.Port, s.ProcessName, s.ProcessPath)
		}
	}
	return nil
}

func runWorkloadSetLabels(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload set-labels", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	var labels labelFlags
	fs.Var(&labels, "label", "label key=value (repeatable; replaces all labels)")
	if err := cli.Parse(fs, args, 1); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall workload set-labels --label key=value ... <workload-id>")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	out := make([]registry.Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, registry.Label{Key: l.Key, Value: l.Value})
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	// Admission, the replacement, and the render a label change triggers
	// are the domain's; the operator surface calls the same function.
	if err := a.fleet.SetLabels(ctx, id, out, ""); err != nil {
		return err
	}
	fmt.Printf("workload %s labels set to [%s]\n", id, labelString(out))
	return nil
}

func runWorkloadSetMode(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload set-mode", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := cli.Parse(fs, args, 2); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return usageError("usage: innerwall workload set-mode <workload-id> visibility|simulation|enforced")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	mode, err := policy.ParseMode(fs.Arg(1))
	if err != nil {
		return err
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	// One workload is a mode change whose selection is that id and whose
	// expected count is one: the same recorded, rendered transaction the
	// operator surface's bulk change runs.
	res, err := a.fleet.ChangeMode(ctx, fleet.ModeChangeRequest{WorkloadIDs: []identity.WorkloadID{id}, TargetMode: mode, ExpectedMatchCount: 1}, true)
	if err != nil {
		return err
	}
	fmt.Printf("workload %s mode set to %s (mode change %s; %d updated)\n", id, policy.ModeName(mode), res.ID, res.DesiredUpdated)
	return nil
}

func runWorkloadResendSnapshot(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload resend-snapshot", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := cli.Parse(fs, args, 1); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall workload resend-snapshot <workload-id>")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	// The refusals and the directive over the notification bridge are
	// the domain's; the operator surface calls the same function. This
	// process holds no streams: the replica holding the workload's
	// stream, if any, sends the directive.
	res, err := a.fleet.RequestReconnect(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("workload %s directed to reconnect; last snapshot sent %s\n", id, ago(res.LastSnapshotSentAt, time.Now()))
	fmt.Println("delivery is not confirmed: the snapshot instant in `innerwall workload status` advances when the agent has reconnected")
	return nil
}

// runWorkloadRetire retires one workload through the domain function the
// operator surface's POST /workloads/{id}/retire calls (ADR-0023). This
// process holds no streams: the directive it commits reaches the replica
// holding the workload's stream, and the stream's next heartbeat bounds it
// if the directive is lost.
func runWorkloadRetire(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload retire", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := cli.Parse(fs, args, 1); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall workload retire <workload-id>")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	res, err := a.fleet.Retire(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("workload %s retired at %s; %d workloads' policy versions advanced\n", id, res.RetiredAt.UTC().Format(time.RFC3339), len(res.Rendered.Changed))
	if g := res.Closed; g != nil {
		fmt.Printf("its agent's silence since %s is stored as an evidence gap\n", g.From.UTC().Format(time.RFC3339))
	}
	fmt.Println("retirement is one way: the credential is refused from now on, and the host returns only by enrolling again as a new workload")
	return nil
}

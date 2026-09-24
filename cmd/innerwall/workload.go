package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

func runWorkload(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall workload list|status|set-labels|set-mode [flags]")
	}
	switch args[0] {
	case "list":
		return runWorkloadList(ctx, args[1:])
	case "status":
		return runWorkloadStatus(ctx, args[1:])
	case "set-labels":
		return runWorkloadSetLabels(ctx, args[1:])
	case "set-mode":
		return runWorkloadSetMode(ctx, args[1:])
	default:
		return usageError("unknown workload command %q (list|status|set-labels|set-mode)", args[0])
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
	if err := fs.Parse(args); err != nil {
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
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d/%s\t%s\n", wl.ID, wl.Hostname, labelString(wl.Labels), policy.ModeName(wl.Mode), syncStateName(*wl), wl.AppliedVersion, latest, ago(wl.LastSeenAt, now))
	}
	return w.Flush()
}

func runWorkloadStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall workload status", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
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
	fmt.Printf("sync state      %s\n", syncStateName(*wl))
	if wl.SyncError != "" {
		fmt.Printf("sync error      %s\n", wl.SyncError)
	}
	fmt.Printf("applied version %d\n", wl.AppliedVersion)
	fmt.Printf("latest version  %d\n", latest)
	fmt.Printf("last seen       %s\n", ago(wl.LastSeenAt, now))
	fmt.Printf("enrolled        %s\n", wl.EnrolledAt.UTC().Format(time.RFC3339))
	fmt.Printf("credential      expires %s\n", wl.CredentialExpiresAt.UTC().Format(time.RFC3339))
	fmt.Printf("agent           %s\n", wl.Agent.Version)
	fmt.Printf("dropped flows   %d\n", wl.DroppedFlowRecords)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall workload set-labels <workload-id> --label key=value ...")
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
	if err := fs.Parse(args); err != nil {
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

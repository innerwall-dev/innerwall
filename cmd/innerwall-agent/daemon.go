package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	"github.com/innerwall-dev/innerwall/internal/agent/collect/conntrack"
	"github.com/innerwall-dev/innerwall/internal/agent/collect/nflog"
	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce/nft"
	"github.com/innerwall-dev/innerwall/internal/agent/inventory"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// runDaemon runs the agent: the sync stream with its apply loop into the
// owned nftables table, periodic inventory and heartbeats, the credential
// renewal timer, and the flow collection loop with its reporter. Before
// dialing the control plane it re-applies the policy it last
// acknowledged, from disk, so a restart or reboot changes nothing on the
// host (ADR-0011). It exits when the credential expires or the control
// plane directs re-enrollment, both of which need an operator with a new
// provisioning token; it never enrolls itself. Exiting leaves the kernel
// rules in place; only `innerwall-agent down` removes them.
func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall-agent daemon", flag.ContinueOnError)
	server := fs.String("server", "", "control-plane address, host:port")
	stateDir := fs.String("state-dir", stateDirDefault(), "directory holding the key, credential, bundle, and last applied policy")
	bufferRecords := fs.Int("flow-buffer-records", collect.DefaultBufferRecords, "flow records held in memory while the control plane is unreachable; the oldest are dropped beyond this")
	table := fs.String("nft-table", nft.DefaultTable, "name of the owned nftables table")
	nflogGroup := fs.Uint("nflog-group", uint(nft.DefaultNflogGroup), "netlink log group the terminal rule logs to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("--server is required")
	}
	if *nflogGroup > 65535 {
		return errors.New("--nflog-group must be at most 65535")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)

	holder, err := credential.LoadHolder(credential.Store{Dir: *stateDir})
	if err != nil {
		return err
	}
	leaf := holder.Leaf()
	log.Info("agent starting", "version", version, "server", *server, "credential_expires", leaf.NotAfter)

	// Enforcement: the persisted policy is re-applied before anything is
	// dialed. A corrupt file applies nothing and is reported; the kernel
	// keeps whatever it holds. A failed re-apply is reported and the
	// daemon carries on: the next snapshot from the control plane is the
	// retry, and it is acknowledged FAILED if the kernel still refuses.
	store := nft.New(nft.Config{StateDir: *stateDir, Table: *table, NflogGroup: uint16(*nflogGroup), Log: log.With("loop", "reconcile")})
	if err := store.Load(); err != nil {
		if errors.Is(err, enforce.ErrCorruptPolicyFile) {
			log.Error("PERSISTED POLICY IS CORRUPT; applying nothing from it, the kernel keeps its current rules until the control plane sends a snapshot", "error", err)
		} else {
			log.Error("reading persisted policy", "error", err)
		}
	} else if cur := store.Current(); cur != nil {
		if err := store.Restore(ctx); err != nil {
			log.Error("re-applying persisted policy failed; the kernel keeps its current rules", "version", cur.GetVersion(), "error", err)
		}
	} else {
		log.Info("no persisted policy; the host stays as it is until the first snapshot")
	}

	renewer := &credential.Renewer{Holder: holder, Server: *server, Log: log.With("loop", "renew")}

	// Collection: conntrack events classified by connection mark, log
	// events from the terminal rule, aggregated per window, buffered,
	// reported on a connection of their own so telemetry never shares the
	// sync stream's fate (ADR-0015).
	buffer := collect.NewBuffer(*bufferRecords)
	collector := &collect.Collector{
		Source: collect.Sources{
			&conntrack.Source{Log: log.With("loop", "collect"), Classify: store.Classify},
			&nflog.Source{Group: uint16(*nflogGroup), Decide: store.TerminalDecision, Log: log.With("loop", "collect")},
		},
		Buffer: buffer,
		Log:    log.With("loop", "collect"),
	}
	reporter := &collect.Reporter{
		Buffer: buffer,
		Dial: func(ctx context.Context) (innerwallv1.AgentServiceClient, io.Closer, error) {
			return agentsync.DialGRPC(ctx, *server, holder)
		},
		Log: log.With("loop", "report"),
	}

	daemon := agentsync.New(agentsync.Config{
		Server:             *server,
		Holder:             holder,
		Store:              store,
		AgentVersion:       version,
		Facts:              inventory.Facts,
		ListeningServices:  inventory.ListeningServices,
		DroppedFlowRecords: buffer.Dropped,
		RenewalError:       renewer.LastError,
		OnSyncConfig: func(cfg *innerwallv1.SyncConfig) {
			collector.SetConfig(cfg)
			reporter.SetConfig(cfg)
		},
		Log: log.With("loop", "sync"),
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	go func() { errc <- daemon.Run(ctx) }()
	go func() { errc <- renewer.Run(ctx) }()
	// The collection loops return only when ctx ends; a failing source
	// is restarted inside them and reported, never fatal.
	go func() { _ = collector.Run(ctx) }()
	go func() { _ = reporter.Run(ctx) }()
	select {
	case <-ctx.Done():
		log.Info("agent stopping; the kernel rules stay in place (innerwall-agent down removes them)")
		return nil
	case err := <-errc:
		return err
	}
}

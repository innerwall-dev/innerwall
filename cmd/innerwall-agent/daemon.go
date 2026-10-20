package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	"github.com/innerwall-dev/innerwall/internal/agent/collect/conntrack"
	"github.com/innerwall-dev/innerwall/internal/agent/collect/nflog"
	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce/nft"
	"github.com/innerwall-dev/innerwall/internal/agent/inventory"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	"github.com/innerwall-dev/innerwall/internal/cli"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// runDaemon runs the agent: the sync stream with its apply loop into the
// owned nftables table, periodic inventory and heartbeats, the credential
// renewal timer, and the flow collection loop with its reporter. Before
// dialing the control plane it re-applies the policy it last
// acknowledged, from disk, so a restart or reboot changes nothing on the
// host (ADR-0011). An expired credential is never a reason to exit, at
// start or later: the daemon keeps enforcing from disk and keeps retrying
// renewal, which the control plane accepts within its renewal grace, and
// everything else resumes on the fresh credential (ADR-0016 as amended).
// It exits when the control plane directs re-enrollment, or when the host
// was enrolled again underneath it; it never enrolls itself. Exiting
// leaves the kernel rules in place; only `innerwall-agent down` removes
// them.
func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall-agent daemon", flag.ContinueOnError)
	server := fs.String("server", "", "control-plane address, host:port")
	stateDir := fs.String("state-dir", stateDirDefault(), "directory holding the key, credential, bundle, and last applied policy")
	bufferRecords := fs.Int("flow-buffer-records", collect.DefaultBufferRecords, "flow records held in memory while the control plane is unreachable; the oldest are dropped beyond this")
	windowKeys := fs.Int("flow-window-keys", collect.DefaultWindowKeys, "distinct flow keys one open aggregation window holds; observations for new keys past it are dropped until the window closes, and the loss is recorded as an evidence gap")
	netlinkBuffer := fs.Int("flow-netlink-buffer", collect.DefaultNetlinkBuffer, "receive buffer, in bytes, of each flow source's netlink socket; a larger buffer rides out longer bursts before the kernel drops events, and every drop is recorded as an evidence gap. 0 keeps the kernel default")
	dumpMax := fs.Int("flow-dump-max", collect.DefaultBufferRecords, "connection-table entries processed from the dump at each conntrack subscribe, and connections tracked to count each once; entries past it are skipped and recorded as an evidence gap. Bounds processing, not the dump's peak memory")
	table := fs.String("nft-table", nft.DefaultTable, "name of the owned nftables table")
	nflogGroup := fs.Uint("nflog-group", uint(nft.DefaultNflogGroup), "netlink log group the terminal rule logs to")
	if err := cli.Parse(fs, args, 0); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("--server is required")
	}
	if *netlinkBuffer < 0 {
		return errors.New("--flow-netlink-buffer must not be negative")
	}
	if *dumpMax <= 0 {
		return errors.New("--flow-dump-max must be positive")
	}
	if *windowKeys <= 0 {
		return errors.New("--flow-window-keys must be positive")
	}
	if *nflogGroup > 65535 {
		return errors.New("--nflog-group must be at most 65535")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	return daemon(ctx, daemonOptions{
		server: *server, stateDir: *stateDir, bufferRecords: *bufferRecords, windowKeys: *windowKeys,
		netlinkBuffer: *netlinkBuffer, dumpMax: *dumpMax, table: *table, nflogGroup: uint16(*nflogGroup), log: log,
	})
}

// daemonOptions are the daemon's validated flags, and the seams a test
// substitutes for the host: the nft runner and the flow sources.
type daemonOptions struct {
	server, stateDir                                  string
	bufferRecords, windowKeys, netlinkBuffer, dumpMax int
	table                                             string
	nflogGroup                                        uint16
	log                                               *slog.Logger
	// runner executes nft; the nft binary when nil.
	runner nft.Runner
	// sources replaces the flow sources (the connection tracker and the
	// terminal rule's log) when non-nil.
	sources collect.Source
}

// daemon runs the agent as runDaemon describes, from validated options.
func daemon(ctx context.Context, o daemonOptions) error {
	log := o.log

	holder, err := credential.LoadHolder(credential.Store{Dir: o.stateDir})
	if err != nil {
		return err
	}
	leaf := holder.Leaf()
	log.Info("agent starting", "version", version, "server", o.server, "credential_expires", leaf.NotAfter)
	if !time.Now().Before(leaf.NotAfter) {
		log.Warn("the stored credential has expired; enforcing from the persisted policy and retrying renewal until it succeeds", "expired_at", leaf.NotAfter)
	}

	// Enforcement: the persisted policy is re-applied before anything is
	// dialed. A corrupt file applies nothing and is reported; the kernel
	// keeps whatever it holds. A failed re-apply is reported and the
	// daemon carries on: the next snapshot from the control plane is the
	// retry, and it is acknowledged FAILED if the kernel still refuses.
	store := nft.New(nft.Config{Runner: o.runner, StateDir: o.stateDir, Table: o.table, NflogGroup: o.nflogGroup, Log: log.With("loop", "reconcile")})
	// What the sources, the open window, and the buffer know they lost is
	// recorded as evidence gaps and shipped with the windows; so is the
	// time the owned table is gone from the kernel, which every heartbeat
	// reads and reports (issue #64).
	gaps := &collect.Gaps{Log: log.With("loop", "collect")}
	presence := &enforce.Presence{
		Probe:     store.TablePresent,
		Installed: func() bool { return store.Current() != nil },
		Removed:   func(since time.Time) { gaps.Open(collect.GapNoSource, collect.GapTableRemoved, since) },
		Restored:  func(at time.Time) { gaps.Close(collect.GapNoSource, at) },
		Log:       log.With("loop", "sync"),
	}
	if err := store.Load(); err != nil {
		if errors.Is(err, enforce.ErrCorruptPolicyFile) {
			log.Error("PERSISTED POLICY IS CORRUPT; applying nothing from it, the kernel keeps its current rules until the control plane sends a snapshot", "error", err)
		} else {
			log.Error("reading persisted policy", "error", err)
		}
	} else if cur := store.Current(); cur != nil {
		if err := store.Restore(ctx); err != nil {
			log.Error("re-applying persisted policy failed; the kernel keeps its current rules", "version", cur.GetVersion(), "error", err)
		} else {
			presence.MarkInstalled(time.Now())
		}
	} else {
		log.Info("no persisted policy; the host stays as it is until the first snapshot")
	}

	renewer := &credential.Renewer{Holder: holder, Server: o.server, Log: log.With("loop", "renew")}

	// Collection: conntrack events classified by connection mark, log
	// events from the terminal rule, aggregated per window, buffered,
	// reported on a connection of their own so telemetry never shares the
	// sync stream's fate (ADR-0015).
	buffer := collect.NewBuffer(o.bufferRecords)
	buffer.Gaps = gaps
	sources := o.sources
	if sources == nil {
		sources = collect.Sources{
			&conntrack.Source{Log: log.With("loop", "collect"), Classify: store.Classify, Gaps: gaps, ReadBuffer: o.netlinkBuffer, DumpMax: o.dumpMax},
			&nflog.Source{Group: o.nflogGroup, Decide: store.TerminalDecision, Log: log.With("loop", "collect"), Gaps: gaps, ReadBuffer: o.netlinkBuffer},
		}
	}
	collector := &collect.Collector{
		Source:     sources,
		Buffer:     buffer,
		Gaps:       gaps,
		WindowKeys: o.windowKeys,
		Log:        log.With("loop", "collect"),
	}
	reporter := &collect.Reporter{
		Buffer: buffer,
		Gaps:   gaps,
		Dial: func(ctx context.Context) (innerwallv1.AgentServiceClient, io.Closer, error) {
			return agentsync.DialGRPC(ctx, o.server, holder)
		},
		Log: log.With("loop", "report"),
	}

	daemon := agentsync.New(agentsync.Config{
		Server:             o.server,
		Holder:             holder,
		Store:              enforce.Watched{PolicyStore: store, Presence: presence},
		AgentVersion:       version,
		Facts:              inventory.Facts,
		ListeningServices:  inventory.ListeningServices,
		DroppedFlowRecords: buffer.Dropped,
		SourceOverruns:     gaps.Overruns,
		RenewalError:       renewer.LastError,
		OwnedTable:         presence.Check,
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

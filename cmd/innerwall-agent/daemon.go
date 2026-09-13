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
	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	"github.com/innerwall-dev/innerwall/internal/agent/inventory"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// runDaemon runs the agent: the sync stream with its apply loop, periodic
// inventory and heartbeats, the credential renewal timer, and the flow
// collection loop with its reporter. It exits when the credential expires
// or the control plane directs re-enrollment, both of which need an
// operator with a new provisioning token; it never enrolls itself.
// Enforcement lands behind the policy store in a later milestone; today
// the store is in memory and every applied change is logged.
func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall-agent daemon", flag.ContinueOnError)
	server := fs.String("server", "", "control-plane address, host:port")
	stateDir := fs.String("state-dir", envOr(envStateDir, defaultStateDir), "directory holding the key, credential, and bundle")
	bufferRecords := fs.Int("flow-buffer-records", collect.DefaultBufferRecords, "flow records held in memory while the control plane is unreachable; the oldest are dropped beyond this")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("--server is required")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)

	holder, err := credential.LoadHolder(credential.Store{Dir: *stateDir})
	if err != nil {
		return err
	}
	leaf := holder.Leaf()
	log.Info("agent starting", "version", version, "server", *server, "credential_expires", leaf.NotAfter)

	store := &enforce.MemoryStore{}
	renewer := &credential.Renewer{Holder: holder, Server: *server, Log: log.With("loop", "renew")}

	// Collection: conntrack events, aggregated per window, buffered,
	// reported on a connection of their own so telemetry never shares the
	// sync stream's fate (ADR-0015).
	buffer := collect.NewBuffer(*bufferRecords)
	collector := &collect.Collector{
		Source: &conntrack.Source{Log: log.With("loop", "collect")},
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
		log.Info("agent stopping")
		return nil
	case err := <-errc:
		return err
	}
}

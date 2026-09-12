package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	"github.com/innerwall-dev/innerwall/internal/agent/inventory"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
)

// runDaemon runs the agent: the sync stream with its apply loop, periodic
// inventory and heartbeats, and the credential renewal timer. It exits
// when the credential expires or the control plane directs re-enrollment,
// both of which need an operator with a new provisioning token; it never
// enrolls itself. Enforcement lands behind the policy store in a later
// milestone; today the store is in memory and every applied change is
// logged.
func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall-agent daemon", flag.ContinueOnError)
	server := fs.String("server", "", "control-plane address, host:port")
	stateDir := fs.String("state-dir", envOr(envStateDir, defaultStateDir), "directory holding the key, credential, and bundle")
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
	daemon := agentsync.New(agentsync.Config{
		Server:            *server,
		Holder:            holder,
		Store:             store,
		AgentVersion:      version,
		Facts:             inventory.Facts,
		ListeningServices: inventory.ListeningServices,
		Log:               log.With("loop", "sync"),
	})
	renewer := &credential.Renewer{Holder: holder, Server: *server, Log: log.With("loop", "renew")}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	go func() { errc <- daemon.Run(ctx) }()
	go func() { errc <- renewer.Run(ctx) }()
	select {
	case <-ctx.Done():
		log.Info("agent stopping")
		return nil
	case err := <-errc:
		return err
	}
}

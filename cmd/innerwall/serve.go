package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/cli"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/ingest"
	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/ui"
)

const (
	serverCertFile = "server.crt"
	serverKeyFile  = "server.key"
)

// renderer narrows the engine to the authoring service's interface.
type renderer struct{ eng *compiler.Engine }

func (r renderer) Render(ctx context.Context) error {
	_, err := r.eng.Render(ctx)
	return err
}

// checkAdvertisedAddress accepts an empty address (none configured) or a
// host:port with a non-empty host and a port in range, the form the
// agent's --server takes. It is the operator's statement of where agents
// reach the gateway; nothing here resolves or probes it.
func checkAdvertisedAddress(addr string) error {
	if addr == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("want host:port: %w", err)
	}
	if host == "" {
		return errors.New("want host:port: the host is empty")
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return fmt.Errorf("want host:port: port %q is not in 1-65535", port)
	}
	return nil
}

// consoleServed reports whether the embedded tree holds a console entry
// point, for the startup log.
func consoleServed(tree fs.FS) bool {
	if tree == nil {
		return false
	}
	_, err := fs.Stat(tree, "index.html")
	return err == nil
}

// gatewayTLS is where serve finds the agent gateway's credentials.
type gatewayTLS struct {
	CADir  string
	InitCA bool
	// Hosts name the server certificate InitCA issues.
	Hosts []string
	// CertFile and KeyFile default to server.crt and server.key in CADir.
	CertFile, KeyFile string
}

// gatewayCredentials composes the agent gateway's side of serve from the
// signing authority backend. With InitCA it first creates the authority,
// and the server certificate through the authority's ServerIssuer, when the
// directory holds none; an existing authority and certificate are kept.
// It returns the authority enrollment signs through and the listener's TLS
// configuration: the server certificate on disk, presented to agents, and
// the authority's bundle, which verifies theirs.
func gatewayCredentials(ctx context.Context, backend authorityBackend, cfg gatewayTLS, log *slog.Logger) (ca.Authority, *tls.Config, error) {
	if cfg.InitCA {
		if err := initAuthority(ctx, backend, cfg.CADir, cfg.Hosts, fileca.DefaultRootTTL, defaultServerTTL, log); err != nil && !errors.Is(err, fileca.ErrExists) {
			return nil, nil, err
		}
	}
	authority, err := backend.Open(cfg.CADir)
	if err != nil {
		return nil, nil, err
	}
	bundle, err := authority.Authority.Bundle(ctx)
	if err != nil {
		return nil, nil, err
	}
	if cfg.CertFile == "" {
		cfg.CertFile = filepath.Join(cfg.CADir, serverCertFile)
	}
	if cfg.KeyFile == "" {
		cfg.KeyFile = filepath.Join(cfg.CADir, serverKeyFile)
	}
	serverCert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("loading server certificate: %w", err)
	}
	tlsCfg, err := gateway.ServerTLSConfig(serverCert, bundle)
	if err != nil {
		return nil, nil, err
	}
	return authority.Authority, tlsCfg, nil
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall serve", flag.ContinueOnError)
	listen := fs.String("listen", defaultListen, "address for the agent gateway (gRPC over TLS)")
	operatorListen := fs.String("operator-listen", defaultOperatorListen, "address for the operator surface (REST/JSON façade and console, TLS only)")
	operatorTLSCert := fs.String("operator-tls-cert", "", "operator listener certificate PEM (with --operator-tls-key; default: a self-signed certificate persisted in <ca-dir>)")
	operatorTLSKey := fs.String("operator-tls-key", "", "operator listener key PEM (with --operator-tls-cert)")
	site := fs.String("site", envOr(envSite, ""), "site label the console header shows (default $"+envSite+")")
	gatewayAddr := fs.String("gateway-advertise-address", envOr(envGateway, ""), "host:port agents reach the agent gateway at, shown in the console's enroll command; unset leaves the console's placeholder (default $"+envGateway+")")
	caDir := fs.String("ca-dir", envOr(envCADir, defaultCADir), "directory holding the signing authority (see `innerwall ca init`)")
	tlsCert := fs.String("tls-cert", "", "server certificate PEM (default <ca-dir>/server.crt)")
	tlsKey := fs.String("tls-key", "", "server key PEM (default <ca-dir>/server.key)")
	leafTTL := fs.Duration("leaf-ttl", enroll.DefaultLeafTTL, "lifetime of issued workload credentials")
	renewalGrace := fs.Duration("renewal-grace", enroll.DefaultRenewalGrace, "how long after its expiry a workload credential may still renew, and do nothing else; beyond it the workload re-enrolls")
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	migrate := fs.Bool("migrate", false, "apply pending database migrations before serving")
	initCA := fs.Bool("init-ca", false, "create the signing authority and server certificate if the CA directory holds none (development convenience)")
	tlsHosts := fs.String("tls-hosts", "localhost,127.0.0.1", "hosts for the server certificate generated by --init-ca and for a generated operator certificate")
	flowRetention := fs.Duration("flow-retention", flowstore.DefaultRetention, "age beyond which stored flow windows are deleted; totals are kept")
	flowRetentionInterval := fs.Duration("flow-retention-interval", flowstore.DefaultRetentionInterval, "period between flow retention runs")
	if err := cli.Parse(fs, args, 0); err != nil {
		return err
	}
	if *renewalGrace <= 0 {
		return errors.New("--renewal-grace must be positive")
	}
	if err := checkAdvertisedAddress(*gatewayAddr); err != nil {
		return fmt.Errorf("--gateway-advertise-address: %w", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)

	url, err := databaseURL(*dbURL)
	if err != nil {
		return err
	}
	if *migrate {
		if err := store.Migrate(ctx, url); err != nil {
			return err
		}
		log.Info("migrations applied")
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer st.Close()

	authority, tlsCfg, err := gatewayCredentials(ctx, fileBackend, gatewayTLS{
		CADir:    *caDir,
		InitCA:   *initCA,
		Hosts:    strings.Split(*tlsHosts, ","),
		CertFile: *tlsCert,
		KeyFile:  *tlsKey,
	}, log)
	if err != nil {
		return err
	}

	// The operator listener serves TLS or it does not serve (ADR-0021): a
	// configured pair is loaded, and absent one a self-signed certificate
	// is generated on first start and persisted beside the authority.
	operatorCert, err := api.ListenerCertificate(*operatorTLSCert, *operatorTLSKey, *caDir, strings.Split(*tlsHosts, ","), time.Now(), log.With("listener", "operator"))
	if err != nil {
		return err
	}
	svc := &enroll.Service{Store: st, Authority: authority, LeafTTL: *leafTTL, RenewalGrace: *renewalGrace}
	engine := &compiler.Engine{Store: st, Log: log}
	operators := &operator.Service{Store: st}
	// Ingest judges clock skew, and the read model whether a workload's
	// telemetry is live, against the flow window the agents are handed, so
	// all three read the one configuration.
	syncCfg := gateway.DefaultSyncConfig()
	flowWindow := syncFlowWindow(syncCfg)
	reads := &readmodel.Reader{Store: st, Flows: st.Flows(), FlowWindow: flowWindow}
	// The console is served at every path outside the API prefix; a
	// build without one (the noconsole tag, or a checkout where `make
	// console` has not run) answers those paths with a marked problem.
	console := ui.Console()
	// The write endpoints call the same domain functions the command
	// line's authoring commands call (ADR-0007 as amended).
	authoring := &policy.Authoring{Store: st, Renderer: renderer{engine}}
	srv := gateway.New(gateway.Deps{
		Enroll:   svc,
		Registry: st,
		Policies: st,
		Engine:   engine,
		Events:   st,
		Ingest: &ingest.Service{
			Directory: st, Flows: st.Flows(), Clock: st, Reports: st,
			FlowWindow: flowWindow,
			Log:        log.With("service", "ingest"),
		},
		SyncConfig: syncCfg,
		Log:        log,
	})
	// A retirement ends the retired workload's stream directly when this
	// replica holds it, and closes its agent's open silence judged by the
	// flow window the agents are handed (ADR-0023).
	fleetSvc := &fleet.Service{Store: st, Engine: engine, Reads: reads, Directives: st, Streams: srv, FlowWindow: flowWindow, Log: log.With("service", "fleet")}
	// The operator surface is composed explicitly: the authenticator and
	// the domain services are injected here, and the listener's handler is
	// built from the composition the surface returns, its middleware and
	// routes registered in order.
	operatorSurface := api.New(api.Deps{
		Authenticator:  operator.NewAuthenticator(operators),
		Operators:      operators,
		Reads:          reads,
		Authoring:      authoring,
		Fleet:          fleetSvc,
		Enroll:         svc,
		Site:           *site,
		GatewayAddress: *gatewayAddr,
		Console:        console,
		Log:            log.With("service", "api"),
	}).Surface()
	httpServer := api.NewHTTPServer(api.ServerTLSConfig(operatorCert), operatorSurface.Handler())
	retention := &flowstore.Retention{
		Store:    st.Flows(),
		Horizon:  *flowRetention,
		Interval: *flowRetentionInterval,
		// Expired operator sessions ride the same schedule (ADR-0021).
		Pruners: []flowstore.Pruner{flowstore.PrunerFunc(operators.PruneSessions)},
		Log:     log.With("job", "flow-retention"),
	}
	grpcServer := gateway.NewGRPCServer(tlsCfg, srv)
	// Every workload's rendered policy must exist before its stream can
	// be served; a fresh database or one migrated from an earlier schema
	// renders here once.
	if _, err := engine.Render(ctx); err != nil {
		return err
	}

	lis, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", *listen, err)
	}
	operatorLis, err := net.Listen("tcp", *operatorListen)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", *operatorListen, err)
	}
	log.Info("control plane serving", "version", version, "listen", lis.Addr().String(), "operator_listen", operatorLis.Addr().String(), "site", *site, "gateway_advertise_address", *gatewayAddr, "leaf_ttl", leafTTL.String(), "flow_retention", flowRetention.String(), "console", consoleServed(console))

	errc := make(chan error, 2)
	go func() { errc <- grpcServer.Serve(lis) }()
	go func() {
		if err := httpServer.ServeTLS(operatorLis, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	// Route render announcements to live streams until shutdown.
	go func() {
		if err := srv.Run(ctx); err != nil {
			log.Error("policy announcement listener stopped", "error", err)
		}
	}()
	// Prune aged flow windows on a schedule; one replica at a time.
	go func() { _ = retention.Run(ctx) }()
	select {
	case <-ctx.Done():
		log.Info("shutting down")
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done := make(chan struct{})
		go func() { grpcServer.GracefulStop(); close(done) }()
		if err := httpServer.Shutdown(stopCtx); err != nil {
			_ = httpServer.Close()
		}
		select {
		case <-done:
		case <-stopCtx.Done():
			grpcServer.Stop()
		}
		return nil
	case err := <-errc:
		return err
	}
}

// syncFlowWindow is the flow window a sync configuration hands agents:
// what ingest judges clock skew and agent silence by, the read model
// whether telemetry is live, and a retirement which silence it closes.
func syncFlowWindow(cfg *innerwallv1.SyncConfig) time.Duration {
	return time.Duration(cfg.GetFlowAggregationWindowSeconds()) * time.Second
}

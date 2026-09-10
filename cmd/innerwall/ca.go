package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
)

// defaultServerTTL is the lifetime of the listener certificate `ca init`
// issues. It is an operator artifact rotated by re-running the command; a
// year keeps a development install working without ceremony.
const defaultServerTTL = 365 * 24 * time.Hour

func runCA(args []string) error {
	if len(args) == 0 || args[0] != "init" {
		return fmt.Errorf("usage: innerwall ca init [flags]")
	}
	fs := flag.NewFlagSet("innerwall ca init", flag.ContinueOnError)
	caDir := fs.String("ca-dir", envOr(envCADir, defaultCADir), "directory to create the authority in")
	hosts := fs.String("hosts", "localhost,127.0.0.1", "comma-separated DNS names and IPs for the server certificate")
	rootTTL := fs.Duration("root-ttl", fileca.DefaultRootTTL, "lifetime of the root certificate")
	serverTTL := fs.Duration("server-ttl", defaultServerTTL, "lifetime of the server certificate")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	return initAuthority(*caDir, strings.Split(*hosts, ","), *rootTTL, *serverTTL, slog.Default())
}

// initAuthority creates the root (key mode 0600) and issues the listener's
// certificate from it, so agents verify the control plane with the same
// bundle that verifies their own credentials.
func initAuthority(dir string, hosts []string, rootTTL, serverTTL time.Duration, log *slog.Logger) error {
	authority, err := fileca.Init(dir, fileca.InitOptions{TTL: rootTTL})
	if err != nil {
		return err
	}
	certPEM, keyPEM, err := authority.IssueServerCertificate(trimAll(hosts), serverTTL)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, serverKeyFile), keyPEM, 0o600); err != nil {
		return fmt.Errorf("writing server key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, serverCertFile), certPEM, 0o644); err != nil { //nolint:gosec // a certificate is public
		return fmt.Errorf("writing server certificate: %w", err)
	}
	log.Info("signing authority created", "dir", dir, "bundle", filepath.Join(dir, fileca.CertFile), "server_hosts", strings.Join(trimAll(hosts), ","))
	return nil
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

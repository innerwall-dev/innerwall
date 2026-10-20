package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/cli"
)

// defaultServerTTL is the lifetime of the listener certificate `ca init`
// and `ca issue-server` issue. It is an operator artifact rotated by
// `ca issue-server`; a year keeps a development install working without
// ceremony.
const defaultServerTTL = 365 * 24 * time.Hour

// signingAuthority is an authority as the commands consume it: one value per
// contract the ca package defines. Enrollment and renewal sign through
// Authority; the agent gateway's server certificate is issued through
// Server.
type signingAuthority struct {
	Authority ca.Authority
	Server    ca.ServerIssuer
}

// authorityBackend is the composition point for the signing authority, the
// one place the commands name an implementation. Everything past it consumes
// the ca interfaces, so a backend that keeps its key elsewhere replaces
// fileBackend here and nowhere else.
type authorityBackend struct {
	// Init creates an authority in dir and refuses a directory that already
	// holds one (fileca.ErrExists).
	Init func(dir string, rootTTL time.Duration) (signingAuthority, error)
	// Open loads the authority in dir, and fails with fileca.ErrNotFound
	// when the directory holds none.
	Open func(dir string) (signingAuthority, error)
}

// fileBackend is the file-backed authority (ADR-0016): one value serves
// both contracts.
var fileBackend = authorityBackend{
	Init: func(dir string, rootTTL time.Duration) (signingAuthority, error) {
		a, err := fileca.Init(dir, fileca.InitOptions{TTL: rootTTL})
		if err != nil {
			return signingAuthority{}, err
		}
		return signingAuthority{Authority: a, Server: a}, nil
	},
	Open: func(dir string) (signingAuthority, error) {
		a, err := fileca.Open(dir)
		if err != nil {
			return signingAuthority{}, err
		}
		return signingAuthority{Authority: a, Server: a}, nil
	},
}

const caUsage = "usage: innerwall ca init [flags] | innerwall ca issue-server --hosts HOSTS [flags]"

func runCA(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New(caUsage)
	}
	switch args[0] {
	case "init":
		return runCAInit(ctx, args[1:])
	case "issue-server":
		return runCAIssueServer(ctx, args[1:])
	default:
		return errors.New(caUsage)
	}
}

func runCAInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall ca init", flag.ContinueOnError)
	caDir := fs.String("ca-dir", envOr(envCADir, defaultCADir), "directory to create the authority in")
	hosts := fs.String("hosts", "localhost,127.0.0.1", "comma-separated DNS names and IPs for the server certificate")
	rootTTL := fs.Duration("root-ttl", fileca.DefaultRootTTL, "lifetime of the root certificate")
	serverTTL := fs.Duration("server-ttl", defaultServerTTL, "lifetime of the server certificate")
	if err := cli.Parse(fs, args, 0); err != nil {
		return err
	}
	return initAuthority(ctx, fileBackend, *caDir, strings.Split(*hosts, ","), *rootTTL, *serverTTL, slog.Default())
}

func runCAIssueServer(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall ca issue-server", flag.ContinueOnError)
	caDir := fs.String("ca-dir", envOr(envCADir, defaultCADir), "directory holding the signing authority to issue from")
	hosts := fs.String("hosts", "", "comma-separated DNS names and IPs for the server certificate (required)")
	ttl := fs.Duration("ttl", defaultServerTTL, "lifetime of the server certificate")
	if err := cli.Parse(fs, args, 0); err != nil {
		return err
	}
	return issueServer(ctx, fileBackend, *caDir, strings.Split(*hosts, ","), *ttl, slog.Default())
}

// initAuthority creates the root (key mode 0600) and issues the listener's
// certificate from it, so agents verify the control plane with the same
// bundle that verifies their own credentials.
func initAuthority(ctx context.Context, backend authorityBackend, dir string, hosts []string, rootTTL, serverTTL time.Duration, log *slog.Logger) error {
	authority, err := backend.Init(dir, rootTTL)
	if err != nil {
		return err
	}
	if _, err := writeServerCertificate(ctx, authority.Server, dir, trimAll(hosts), serverTTL); err != nil {
		return err
	}
	log.Info("signing authority created", "dir", dir, "bundle", filepath.Join(dir, fileca.CertFile), "server_hosts", strings.Join(trimAll(hosts), ","))
	return nil
}

// issueServer issues a new server certificate for hosts from the authority
// already in dir and puts it in place of the current one. The certificate
// chains to the same root, so every enrolled agent keeps trusting the
// gateway and the bundle is untouched. A running control plane keeps
// presenting the certificate it loaded at start; it presents this one once
// restarted.
func issueServer(ctx context.Context, backend authorityBackend, dir string, hosts []string, ttl time.Duration, log *slog.Logger) error {
	hosts = trimAll(hosts)
	if len(hosts) == 0 {
		return errors.New("--hosts is required: the DNS names and IPs agents dial the gateway at")
	}
	authority, err := backend.Open(dir)
	if errors.Is(err, fileca.ErrNotFound) {
		return fmt.Errorf("no signing authority in %s: issue-server issues from an existing authority; `innerwall ca init` creates one", dir)
	}
	if err != nil {
		return err
	}
	notAfter, err := writeServerCertificate(ctx, authority.Server, dir, hosts, ttl)
	if err != nil {
		return err
	}
	log.Info("server certificate issued; restart the control plane to present it", "dir", dir, "server_hosts", strings.Join(hosts, ","), "not_after", notAfter.UTC().Format(time.RFC3339))
	return nil
}

// writeServerCertificate issues a server certificate through issuer and
// writes it and its key into dir as server.crt and server.key, returning the
// certificate's expiry. Nothing in dir changes unless the issue succeeds.
func writeServerCertificate(ctx context.Context, issuer ca.ServerIssuer, dir string, hosts []string, ttl time.Duration) (time.Time, error) {
	certPEM, keyPEM, err := issuer.IssueServerCertificate(ctx, hosts, ttl)
	if err != nil {
		return time.Time{}, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Time{}, errors.New("issued server certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing issued server certificate: %w", err)
	}
	err = replaceFiles(dir, []stagedFile{
		{name: serverKeyFile, data: keyPEM, mode: 0o600},
		{name: serverCertFile, data: certPEM, mode: 0o644},
	})
	return cert.NotAfter, err
}

type stagedFile struct {
	name string
	data []byte
	mode os.FileMode
}

// replaceFiles writes every file to a new file in dir and renames each into
// place only once all are written, so a failure while writing leaves the
// previous files as they were. Each rename is atomic; a crash between the
// two renames leaves a new key beside the old certificate, which serve
// refuses at start as a mismatched pair and re-running the command repairs.
func replaceFiles(dir string, files []stagedFile) error {
	temps := make([]string, 0, len(files))
	defer func() {
		for _, t := range temps {
			_ = os.Remove(t) // already renamed on success
		}
	}()
	for _, f := range files {
		tmp, err := os.CreateTemp(dir, "."+f.name+".new-*")
		if err != nil {
			return fmt.Errorf("staging %s: %w", f.name, err)
		}
		temps = append(temps, tmp.Name())
		// CreateTemp opens with mode 0600; a key never exists wider.
		if err := writeSynced(tmp, f.data, f.mode); err != nil {
			return fmt.Errorf("staging %s: %w", f.name, err)
		}
	}
	for i, f := range files {
		if err := os.Rename(temps[i], filepath.Join(dir, f.name)); err != nil {
			return fmt.Errorf("replacing %s: %w", f.name, err)
		}
	}
	return nil
}

func writeSynced(f *os.File, data []byte, mode os.FileMode) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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

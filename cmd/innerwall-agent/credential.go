package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

const (
	envToken    = "INNERWALL_TOKEN"
	envStateDir = "INNERWALL_AGENT_STATE_DIR"

	defaultStateDir = "/var/lib/innerwall-agent"
	callTimeout     = 30 * time.Second
)

func runEnroll(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall-agent enroll", flag.ContinueOnError)
	server := fs.String("server", "", "control-plane address, host:port")
	token := fs.String("token", "", "provisioning token (default $"+envToken+", which keeps it out of the process list)")
	bootstrapCA := fs.String("bootstrap-ca", "", "PEM trust anchor for the control plane, delivered out of band with the token; empty uses the system trust store")
	stateDir := fs.String("state-dir", stateDirDefault(), "directory for the key, credential, and bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("--server is required")
	}
	if *token == "" {
		*token = os.Getenv(envToken)
	}
	if *token == "" {
		return fmt.Errorf("a provisioning token is required: --token or $%s", envToken)
	}

	store := credential.Store{Dir: *stateDir}
	if _, err := store.LoadKey(); err == nil {
		return fmt.Errorf("%s already holds a credential; remove it to enroll again", *stateDir)
	}

	var anchor []byte
	if *bootstrapCA != "" {
		// At enrollment the agent has no credential and no bundle, so it
		// cannot verify the control plane with anything it already holds.
		// The anchor travels out of band with the token: the token proves
		// the agent to the server, the anchor proves the server to the
		// agent. Sending the token to an unverified server would hand it
		// to whoever answered.
		b, err := os.ReadFile(*bootstrapCA)
		if err != nil {
			return fmt.Errorf("reading bootstrap ca: %w", err)
		}
		anchor = b
	}

	key, err := credential.GenerateKey()
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	res, err := credential.Enroll(ctx, key, credential.EnrollOptions{
		Server:      *server,
		Token:       *token,
		BootstrapCA: anchor,
		Facts:       &innerwallv1.HostFacts{Hostname: hostname},
	})
	if err != nil {
		return err
	}
	if err := store.Save(key, res.CertificatePEM, res.BundlePEM); err != nil {
		return err
	}
	fmt.Printf("enrolled as %s\n", res.WorkloadID)
	for _, l := range res.Labels {
		fmt.Printf("  label %s=%s\n", l.GetKey(), l.GetValue())
	}
	fmt.Printf("credential stored in %s\n", *stateDir)
	return nil
}

func runRenew(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall-agent renew", flag.ContinueOnError)
	server := fs.String("server", "", "control-plane address, host:port")
	stateDir := fs.String("state-dir", stateDirDefault(), "directory holding the key, credential, and bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("--server is required")
	}
	store := credential.Store{Dir: *stateDir}
	cred, trust, err := store.Load()
	if err != nil {
		return err
	}
	key, err := store.LoadKey()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	res, err := credential.Renew(ctx, key, credential.RenewOptions{Server: *server, Credential: cred, Trust: trust})
	if err != nil {
		return err
	}
	if err := store.ReplaceCertificate(res.CertificatePEM, res.BundlePEM); err != nil {
		return err
	}
	fmt.Printf("renewed credential for %s\n", res.WorkloadID)
	return nil
}

// stateDirDefault is the state directory flag's default: the environment
// override when set, else the packaged location.
func stateDirDefault() string {
	if v := os.Getenv(envStateDir); v != "" {
		return v
	}
	return defaultStateDir
}

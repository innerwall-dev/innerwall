package gateway_test

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/enroll/enrolltest"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// harness is a control plane on a loopback listener: file CA, enrollment
// service over the given store, TLS as configured for production.
type harness struct {
	addr      string
	authority *fileca.Authority
	bundle    []byte
	service   *enroll.Service
}

func newHarness(t *testing.T, st enroll.Store) *harness {
	t.Helper()
	authority, err := fileca.Init(t.TempDir(), fileca.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := authority.Bundle(context.Background())
	certPEM, keyPEM, err := authority.IssueServerCertificate([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg, err := gateway.ServerTLSConfig(serverCert, bundle)
	if err != nil {
		t.Fatal(err)
	}
	svc := &enroll.Service{Store: st, Authority: authority, LeafTTL: time.Hour}
	srv := gateway.NewGRPCServer(tlsCfg, gateway.New(svc, nil))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return &harness{addr: lis.Addr().String(), authority: authority, bundle: bundle, service: svc}
}

// dial opens a connection with an optional client credential, trusting the
// harness bundle.
func (h *harness) dial(t *testing.T, cred *tls.Certificate) *grpc.ClientConn {
	t.Helper()
	cfg, err := credential.TLSConfig(cred, h.bundle)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(h.addr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func wantCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("err = %v, want grpc status %v", err, code)
	}
	if st.Code() != code {
		t.Fatalf("code = %v (%s), want %v", st.Code(), st.Message(), code)
	}
}

func TestAgentServiceRequiresClientCertificate(t *testing.T) {
	h := newHarness(t, enrolltest.NewMemStore())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := h.dial(t, nil)
	key, _ := credential.GenerateKey()
	csr, _ := credential.NewCSR(key)

	// Unary: RenewCredential.
	_, err := innerwallv1.NewAgentServiceClient(conn).RenewCredential(ctx, &innerwallv1.RenewCredentialRequest{CsrPem: csr})
	wantCode(t, err, codes.Unauthenticated)

	// Streaming: Sync. The interceptor must reject before the (still
	// unimplemented) handler is reached.
	stream, err := innerwallv1.NewAgentServiceClient(conn).Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	wantCode(t, err, codes.Unauthenticated)

	// Client-streaming: ReportFlows.
	rf, err := innerwallv1.NewAgentServiceClient(conn).ReportFlows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rf.CloseAndRecv()
	wantCode(t, err, codes.Unauthenticated)
}

func TestAgentServiceRejectsCertificateWithoutIdentity(t *testing.T) {
	h := newHarness(t, enrolltest.NewMemStore())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A certificate the authority signed, but for a server, so it verifies
	// and yet carries no workload URI SAN. The TLS layer accepts it; the
	// interceptor must not.
	certPEM, keyPEM, err := h.authority.IssueServerCertificate([]string{"impostor"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := h.dial(t, &cred)
	key, _ := credential.GenerateKey()
	csr, _ := credential.NewCSR(key)
	_, err = innerwallv1.NewAgentServiceClient(conn).RenewCredential(ctx, &innerwallv1.RenewCredentialRequest{CsrPem: csr})
	// The server certificate lacks the client-auth key usage, so the TLS
	// handshake itself fails; either failure is a rejection, but never a
	// renewal.
	if err == nil {
		t.Fatal("certificate without workload identity was accepted")
	}
}

func TestAgentServiceRejectsCertificateFromForeignAuthority(t *testing.T) {
	h := newHarness(t, enrolltest.NewMemStore())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	foreign, err := fileca.Init(t.TempDir(), fileca.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := credential.GenerateKey()
	csr, _ := credential.NewCSR(key)
	// Sign directly with the foreign authority: a well-formed workload
	// credential from the wrong root.
	certPEM, err := foreign.Sign(ctx, csr, mustID(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, _ := pemKey(key)
	cred, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := h.dial(t, &cred)
	_, err = innerwallv1.NewAgentServiceClient(conn).RenewCredential(ctx, &innerwallv1.RenewCredentialRequest{CsrPem: csr})
	if err == nil {
		t.Fatal("credential from a foreign authority was accepted")
	}
}

func TestEnrollRequiresValidToken(t *testing.T) {
	st := enrolltest.NewMemStore()
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := h.dial(t, nil)
	client := innerwallv1.NewEnrollmentServiceClient(conn)
	key, _ := credential.GenerateKey()
	csr, _ := credential.NewCSR(key)

	for name, tok := range map[string]string{
		"empty":     "",
		"malformed": "iw_short",
		"unknown":   mustToken(t),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := client.Enroll(ctx, &innerwallv1.EnrollRequest{ProvisioningToken: tok, CsrPem: csr})
			wantCode(t, err, codes.Unauthenticated)
		})
	}

	t.Run("expired", func(t *testing.T) {
		plain, _, err := h.service.MintToken(ctx, "expired", nil, time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
		_, err = client.Enroll(ctx, &innerwallv1.EnrollRequest{ProvisioningToken: plain, CsrPem: csr})
		wantCode(t, err, codes.Unauthenticated)
		if st, _ := status.FromError(err); st.Message() != enroll.ErrTokenExpired.Error() {
			t.Fatalf("message = %q", st.Message())
		}
	})

	t.Run("revoked", func(t *testing.T) {
		plain, tok, err := h.service.MintToken(ctx, "revoked", nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.RevokeToken(ctx, tok.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		_, err = client.Enroll(ctx, &innerwallv1.EnrollRequest{ProvisioningToken: plain, CsrPem: csr})
		wantCode(t, err, codes.Unauthenticated)
		if st, _ := status.FromError(err); st.Message() != enroll.ErrTokenRevoked.Error() {
			t.Fatalf("message = %q", st.Message())
		}
	})

	t.Run("valid token but bad csr", func(t *testing.T) {
		plain, _, err := h.service.MintToken(ctx, "ok", nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Enroll(ctx, &innerwallv1.EnrollRequest{ProvisioningToken: plain, CsrPem: []byte("nope")})
		wantCode(t, err, codes.InvalidArgument)
	})
}

package ca

import (
	"context"
	"time"
)

// ServerIssuer issues the TLS server certificate the agent gateway presents.
// It is separate from Authority, whose contract is enrollee credentials
// alone: a server certificate names hosts, not a workload, and is never
// issued through enrollment or renewal. The certificate is signed by the
// same root that Bundle returns, so agents verify the gateway with the
// bundle they already hold; issuing a new one for another host list leaves
// every enrolled agent trusting it.
type ServerIssuer interface {
	// IssueServerCertificate returns a certificate for hosts (DNS names or
	// IP addresses, each a SAN) valid for ttl, and its fresh private key,
	// both PEM-encoded. The authority's own key is never reused for it.
	IssueServerCertificate(ctx context.Context, hosts []string, ttl time.Duration) (certPEM, keyPEM []byte, err error)
}

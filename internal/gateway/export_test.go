package gateway

import (
	"testing"
	"time"
)

// SetCredentialEndLead sets the lead before a credential's expiry at which
// a stream opened on it is directed to reconnect, for the rest of t. Call
// it before the server is started, so every stream reads the new lead; it
// is restored once every later cleanup, the server's included, has run.
func SetCredentialEndLead(t testing.TB, d time.Duration) {
	prev := credentialEndLead
	credentialEndLead = d
	t.Cleanup(func() { credentialEndLead = prev })
}

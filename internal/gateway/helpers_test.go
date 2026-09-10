package gateway_test

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

func mustID(t *testing.T) identity.WorkloadID {
	t.Helper()
	id, err := identity.NewWorkloadID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustToken(t *testing.T) string {
	t.Helper()
	plain, _, err := enroll.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func pemKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

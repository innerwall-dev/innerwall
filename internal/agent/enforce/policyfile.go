package enforce

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// PolicyFileName is the file in the agent state directory that holds the
// last applied policy.
const PolicyFileName = "policy.bin"

// PolicyPath returns the policy file's path inside a state directory.
func PolicyPath(stateDir string) string { return filepath.Join(stateDir, PolicyFileName) }

// ErrCorruptPolicyFile is returned by Load when the file exists but its
// content does not verify. A corrupt file is never partially trusted: the
// caller applies nothing from it and says so loudly (ADR-0011).
var ErrCorruptPolicyFile = errors.New("enforce: persisted policy file is corrupt")

// policyMagic starts every policy file so a foreign file is refused
// before its checksum is even read.
var policyMagic = []byte("innerwall-policy-v1\n")

// PolicyFile persists the last applied WorkloadPolicy so that it survives
// a daemon restart and a host reboot: the agent fails static, re-applying
// on start what it last acknowledged (ADR-0011, ADR-0018). The file is
// the deterministic serialization of the canonical policy, prefixed by a
// magic line and followed by a SHA-256 of the serialization, written
// durably with mode 0600.
type PolicyFile struct {
	Path string
}

// Save writes policy durably (WriteDurable): a reader sees the old file
// or the new one and never a partial write, and once Save returns nil the
// new file survives a crash or a power loss. The store saves before it
// touches the kernel, so a policy is never acknowledged without its
// persisted copy (ADR-0020).
func (f PolicyFile) Save(policy *innerwallv1.WorkloadPolicy) error {
	body, err := rendered.Marshal(policy)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	data := make([]byte, 0, len(policyMagic)+len(body)+len(sum))
	data = append(data, policyMagic...)
	data = append(data, body...)
	data = append(data, sum[:]...)
	return WriteDurable(f.Path, data)
}

// Load reads the persisted policy. It returns nil, nil when no file
// exists, and ErrCorruptPolicyFile when the file exists but does not
// verify.
func (f PolicyFile) Load() (*innerwallv1.WorkloadPolicy, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("enforce: reading policy file: %w", err)
	}
	if len(data) < len(policyMagic)+sha256.Size || string(data[:len(policyMagic)]) != string(policyMagic) {
		return nil, fmt.Errorf("%w: %s", ErrCorruptPolicyFile, f.Path)
	}
	body := data[len(policyMagic) : len(data)-sha256.Size]
	want := data[len(data)-sha256.Size:]
	if sum := sha256.Sum256(body); string(sum[:]) != string(want) {
		return nil, fmt.Errorf("%w: %s: checksum mismatch", ErrCorruptPolicyFile, f.Path)
	}
	p, err := rendered.Unmarshal(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrCorruptPolicyFile, f.Path, err)
	}
	return rendered.Canonical(p), nil
}

// Remove deletes the persisted policy; a missing file is not an error.
func (f PolicyFile) Remove() error {
	if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("enforce: removing policy file: %w", err)
	}
	return nil
}

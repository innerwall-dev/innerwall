package rendered

import (
	"fmt"
	"strings"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// RuleID returns the identity of the resolved rule rendered from authored
// rule authoredID for one protocol. A resolved rule carries exactly one
// protocol while an authored rule may permit several, so an authored rule
// renders to one resolved rule per protocol; the wire contract's rule
// removals and peer changes are keyed by resolved-rule id and therefore
// need each of those to be distinct. The authored id is the prefix, so
// provenance is a string split away (ADR-0018).
func RuleID(authoredID string, p innerwallv1.Protocol) string {
	return authoredID + "/" + protoName(p)
}

// Provenance splits a resolved-rule id into the authored rule id and the
// protocol it was rendered for.
func Provenance(ruleID string) (authoredID string, p innerwallv1.Protocol, err error) {
	authoredID, suffix, ok := strings.Cut(ruleID, "/")
	if !ok || authoredID == "" {
		return "", innerwallv1.Protocol_PROTOCOL_UNSPECIFIED, fmt.Errorf("rendered: rule id %q is not <authored-id>/<protocol>", ruleID)
	}
	v, ok := innerwallv1.Protocol_value["PROTOCOL_"+strings.ToUpper(suffix)]
	if !ok || v == 0 {
		return "", innerwallv1.Protocol_PROTOCOL_UNSPECIFIED, fmt.Errorf("rendered: rule id %q names unknown protocol %q", ruleID, suffix)
	}
	return authoredID, innerwallv1.Protocol(v), nil
}

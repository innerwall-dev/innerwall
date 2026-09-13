package nft

import (
	"fmt"
	"strings"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// RenderDelta returns the script that moves the installed table from
// policy from to policy to when the difference is peer membership only:
// set element additions and deletions, in one transaction. It reports
// false when the difference touches anything else (a rule's protocol or
// ports, a rule added or removed, the mode), in which case the caller
// installs to as a full rendering. Peer membership is the high-churn
// quantity, and a set update leaves every rule and every other set
// untouched (ADR-0015, ADR-0020).
func RenderDelta(from, to *innerwallv1.WorkloadPolicy, opts Options) (string, bool) {
	changes := rendered.Diff(from, to)
	if len(changes) == 0 {
		return "", false
	}
	table := opts.table()
	var b strings.Builder
	for _, ch := range changes {
		switch c := ch.GetChange().(type) {
		case *innerwallv1.DeltaChange_AddPeers:
			writeElements(&b, "add", table, c.AddPeers)
		case *innerwallv1.DeltaChange_RemovePeers:
			writeElements(&b, "delete", table, c.RemovePeers)
		default:
			return "", false
		}
	}
	return b.String(), true
}

func writeElements(b *strings.Builder, verb, table string, pc *innerwallv1.PeerChange) {
	v4, v6 := splitPeers(pc.GetPeerCidrs())
	if len(v4) > 0 {
		fmt.Fprintf(b, "%s element inet %s %s { %s }\n", verb, table, SetName(pc.GetRuleId(), false), strings.Join(v4, ", "))
	}
	if len(v6) > 0 {
		fmt.Fprintf(b, "%s element inet %s %s { %s }\n", verb, table, SetName(pc.GetRuleId(), true), strings.Join(v6, ", "))
	}
}

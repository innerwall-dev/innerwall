package ingest

import (
	"net/netip"
	"sort"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// Index resolves source addresses to peers as the registry stands at one
// instant. It is built once per reported window and discarded, so every
// record in a window is resolved against the same view and no replica
// keeps state between windows (ADR-0017).
type Index struct {
	workloads map[netip.Addr]workloadEntry
	groups    []groupEntry
}

type workloadEntry struct {
	key    string
	labels map[string]string
}

type groupEntry struct {
	prefix netip.Prefix
	key    string
}

// BuildIndex indexes the workloads' current addresses and the address
// groups' CIDRs.
func BuildIndex(workloads []registry.Workload, groups []policy.AddressGroup) *Index {
	idx := &Index{workloads: map[netip.Addr]workloadEntry{}}
	for i := range workloads {
		w := &workloads[i]
		entry := workloadEntry{key: w.ID.String(), labels: w.LabelMap()}
		for _, a := range w.Addresses {
			a = a.Unmap()
			// Two workloads reporting one address is a registry
			// inconsistency; the first in registry order (enrollment
			// time) wins, deterministically.
			if _, taken := idx.workloads[a]; !taken {
				idx.workloads[a] = entry
			}
		}
	}
	for i := range groups {
		g := &groups[i]
		for _, c := range g.CIDRs {
			pfx, err := netip.ParsePrefix(c)
			if err != nil {
				if a, err := netip.ParseAddr(c); err == nil {
					pfx = netip.PrefixFrom(a, a.BitLen())
				} else {
					continue
				}
			}
			idx.groups = append(idx.groups, groupEntry{prefix: pfx.Masked(), key: g.ID.String()})
		}
	}
	// Longest prefix first, then by id, so the most specific group wins
	// and ties resolve the same way every time.
	sort.SliceStable(idx.groups, func(i, j int) bool {
		if idx.groups[i].prefix.Bits() != idx.groups[j].prefix.Bits() {
			return idx.groups[i].prefix.Bits() > idx.groups[j].prefix.Bits()
		}
		return idx.groups[i].key < idx.groups[j].key
	})
	return idx
}

// Resolve returns the peer behind addr. A workload's current address wins
// over any address group containing it; the most specific group wins over
// a wider one; an address matching neither is recorded as itself. The
// workload's labels are copied so the snapshot is independent of later
// registry changes.
func (idx *Index) Resolve(addr netip.Addr) flowstore.Peer {
	addr = addr.Unmap()
	if w, ok := idx.workloads[addr]; ok {
		labels := make(map[string]string, len(w.labels))
		for k, v := range w.labels {
			labels[k] = v
		}
		return flowstore.Peer{Kind: flowstore.PeerWorkload, Key: w.key, Labels: labels}
	}
	for _, g := range idx.groups {
		if g.prefix.Contains(addr) {
			return flowstore.Peer{Kind: flowstore.PeerAddressGroup, Key: g.key}
		}
	}
	return flowstore.Peer{Kind: flowstore.PeerUnknown, Key: addr.String()}
}

// HasWorkload reports whether the index knows a workload by id.
func (idx *Index) HasWorkload(id string) bool {
	for _, w := range idx.workloads {
		if w.key == id {
			return true
		}
	}
	return false
}

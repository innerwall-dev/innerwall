# ADR-0003: Enforce via native OS firewalls with an owned, atomically replaced ruleset

**Status:** Accepted

## Context

Enforcement could be built as a custom datapath (kernel module, packet-processing proxy, overlay) or by programming the packet filter every mainstream OS already ships. A custom datapath adds performance risk, kernel-compatibility burden, and an opaque failure mode to every host. Meanwhile, mutating firewall state that other software also manages is a classic source of rule-clobbering bugs and partial-application outages.

## Decision

- The agent programs the **native host firewall**: nftables on Linux (v1). The Windows filtering platform is the intended second backend behind the same enforcement interface.
- All Innerwall rules live in an **Innerwall-owned nftables table**. The agent never edits rules outside its table and other software's tables are never touched.
- Ruleset application is **atomic**: the owned table is replaced as a single unit. A host runs either compiled version N or version N+1 — never a partially applied mixture.
- Flow collection starts from **conntrack** (universally available, zero kernel dependencies); eBPF-based collection is a later additive backend behind the collector interface, not a prerequisite.

## Consequences

- Near-zero datapath overhead; enforcement failure modes are inspectable with standard OS tooling.
- Coexistence with host-managed firewall rules is clean by construction (table ownership).
- Capability is bounded by what native filters express; that boundary is accepted for the problem domain (L3/L4 segmentation).
- Per-OS enforcement backends are required; the enforcement interface is the seam.

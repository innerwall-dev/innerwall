import type {
	Flow,
	PeerRef,
	RenderedPolicy,
	RenderedRule,
	Workload,
} from "@/api/schema";
import { labelPairs, shortId } from "@/lib/format";

// Authored rules carry no name, only an id and a description, so a rule
// is shown by its description with the short form of its id beside it.
// A flow names its rule by the rendered rule's id; the rendered policy
// supplies the description when the rule is still in it.

export type RuleIndex = Map<string, RenderedRule>;

export function indexRules(policy: RenderedPolicy | null): RuleIndex {
	return new Map((policy?.rules ?? []).map((r) => [r.id, r]));
}

// ruleId is a rendered rule's id in short form: the authored rule's
// short id and the protocol it was split by, aa5b70e7/tcp.
export function ruleId(r: { id: string; authored_rule_id?: string }): string {
	const [authored, ...rest] = r.id.split("/");
	return [shortId(r.authored_rule_id ?? authored ?? r.id), ...rest].join("/");
}

// matchedRule is the flow table's rule cell.
export function matchedRule(
	flow: Flow,
	rules: RuleIndex,
): { text: string; id?: string } {
	if (!flow.rule) {
		return flow.verdict === "observed"
			? { text: "not evaluated (visibility)" }
			: { text: "— (no rule matched)" };
	}
	const r = rules.get(flow.rule.id);
	if (r?.description) return { text: r.description, id: ruleId(r) };
	return { text: "", id: ruleId(flow.rule) };
}

// peerNote says who a flow's source was, as ingestion resolved it. It
// reads the identity the peer carries rather than its kind's spelling.
export function peerNote(peer: PeerRef): string {
	if (peer.workload_id) {
		const labels = labelPairs(peer.labels)
			.map(([k, v]) => `${k}=${v}`)
			.join(" ");
		return [peer.name ?? "workload", labels].filter(Boolean).join(" · ");
	}
	if (peer.address_group_id) return `address group ${peer.name ?? ""}`.trim();
	return "not managed";
}

// covers says whether a rendered rule admits a protocol and port: the
// same protocol, and a port inside one of its ranges or no ranges at all
// (every port).
export function covers(
	r: RenderedRule,
	protocol: string,
	port: number,
): boolean {
	if (r.protocol !== protocol) return false;
	if (r.ports.length === 0) return true;
	return r.ports.some((p) => port >= p.start && port <= p.end);
}

export function coveringRules(
	policy: RenderedPolicy | null,
	svc: Workload["listening_services"][number],
): RenderedRule[] {
	return (policy?.rules ?? []).filter((r) => covers(r, svc.protocol, svc.port));
}

// portsText is a rule's protocol and ports: tcp 8443, tcp 6000-6010, or
// every port.
export function portsText(r: RenderedRule): string {
	if (r.protocol === "icmp") return "icmp";
	if (r.ports.length === 0) return `${r.protocol} all ports`;
	return `${r.protocol} ${r.ports
		.map((p) => (p.start === p.end ? `${p.start}` : `${p.start}-${p.end}`))
		.join(", ")}`;
}

// peersText is a rule's peers: a count of host routes when every peer
// is a single address, the prefixes otherwise.
export function peersText(r: RenderedRule): string {
	if (r.peer_cidrs.length === 0) return "no peers";
	const hosts = r.peer_cidrs.filter(
		(c) => c.endsWith("/32") || c.endsWith("/128"),
	);
	if (hosts.length === r.peer_cidrs.length && hosts.length > 1) {
		const v4 = hosts.every((c) => c.endsWith("/32"));
		return `${hosts.length} host routes (${v4 ? "/32" : "/32, /128"})`;
	}
	return r.peer_cidrs.join(", ");
}

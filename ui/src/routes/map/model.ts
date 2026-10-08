import type {
	AddressGroup,
	EvidenceGap,
	LabelMap,
	Mode,
	PeerRef,
	Rollup,
	RollupGroup,
	Verdict,
	Workload,
	WorkloadRef,
} from "@/api/schema";
import { workloadsWith } from "@/lib/gaps";

// The flow map's model: the rollup of stored windows grouped by source
// peer and destination workload, folded into label groups. A workload
// belongs to the group named by its value for the grouping key; peers
// that are not workloads keep the identity ingestion resolved them to.
// Nothing here is recomputed from raw flows: every count is a sum of
// the rollup's own groups, and the peer of every row is what ingestion
// stored.

// precedence ranks the decisions an edge can carry, most consequential
// first. An edge whose pairs received several decisions is drawn as the
// first of them present: traffic that was dropped, or would be, is never
// hidden under traffic that was admitted or merely observed.
export const precedence: Verdict[] = [
	"blocked",
	"would_block",
	"allowed",
	"observed",
];

// NodeKind is how a node is drawn. Managed nodes are label groups of
// enrolled workloads; an unlabeled node is the workloads that carry no
// labels at all (which no selector can match); a keyless node is the
// workloads that carry labels but none under the grouping key.
// Unmanaged nodes are address groups and the peers that resolved to
// nothing ingestion knows; they only ever appear as sources, because no
// agent observes traffic into them.
export type NodeKind =
	| "managed"
	| "keyless"
	| "unlabeled"
	| "address-group"
	| "unknown";

export interface MapNode {
	id: string;
	kind: NodeKind;
	title: string;
	// The label requirement a managed node stands for, `key=value`.
	selector?: string;
	// Managed kinds: every member workload known, in scope or seen as a
	// source, and how many of the listed members run in each mode.
	workloadIds: string[];
	modes: Partial<Record<Mode, number>>;
	// Members seen only as sources outside the listed scope; their mode
	// is not read.
	unlisted: number;
	// Address groups: the group and its CIDRs.
	addressGroupId?: string;
	cidrs?: string[];
	// Unknown peers: addresses no address group contains, and stored
	// keys of a kind this console does not recognize, kept apart so they
	// are never shown as if they were addresses.
	addresses: string[];
	unrecognized: string[];
}

// Pair is one row of the rollup: one stored source peer into one
// destination workload, under one decision.
export interface Pair {
	src: PeerRef;
	dst: WorkloadRef;
	// peerKey is the stored peer key GET /flows filters on.
	peerKey: string;
	verdict: Verdict;
	flows: number;
	connections: number;
	bytes: number;
	firstSeen: string;
	lastSeen: string;
}

export interface DecisionTotals {
	connections: number;
	flows: number;
	lastSeen: string;
}

export interface MapEdge {
	id: string;
	source: string;
	target: string;
	decision: Verdict;
	byDecision: Partial<Record<Verdict, DecisionTotals>>;
	connections: number;
	lastSeen: string;
	pairs: Pair[];
}

export interface MapModel {
	nodes: MapNode[];
	edges: MapEdge[];
	// Connections across the whole range, from the rollups' totals, which
	// count every group whether or not it was returned.
	connections: number;
	// Workloads that reported inbound flows in the range: distinct
	// destinations among the returned groups. A lower bound when a
	// rollup was truncated.
	reporting: number;
	truncated: boolean;
	// Workloads whose agents reported evidence gaps in the range, with
	// the gaps: the map cannot show what was lost there.
	gapped: EvidenceGap["workload"][];
	gaps: EvidenceGap[];
	// Listed workloads whose agents have dropped flow records and whose
	// loss no gap in the range already places: the counter is live, not
	// scoped to the range, so these may be incomplete.
	dropped: Workload[];
	effectiveFrom: string | null;
	effectiveTo: string | null;
}

// RollupByDecision is one `src,dst` rollup per decision, each filtered
// by `verdict`: the grouping itself carries no decision.
export type RollupByDecision = Partial<Record<Verdict, Rollup>>;

export const unlabeledId = "unlabeled";
export const keylessId = "keyless";
export const unknownId = "unknown";

// groupOf names the node a workload belongs to under a grouping key.
export function groupOf(labels: LabelMap, key: string): string {
	if (Object.keys(labels).length === 0) return unlabeledId;
	const v = labels[key];
	return v === undefined ? keylessId : `g:${v}`;
}

// peerKey is the stored key of a peer, the form GET /flows filters on.
export function peerKey(p: PeerRef): string {
	switch (p.kind) {
		case "workload":
			return p.workload_id ?? "";
		case "group":
			return p.address_group_id ?? "";
		default:
			return p.address ?? "";
	}
}

function peerNodeId(p: PeerRef, key: string): string {
	switch (p.kind) {
		case "workload":
			return groupOf(p.labels, key);
		case "group":
			return `ag:${p.address_group_id ?? ""}`;
		default:
			return unknownId;
	}
}

function later(a: string, b: string): string {
	return Date.parse(a) >= Date.parse(b) ? a : b;
}

// buildModel folds the per-decision rollups, the listed workloads in
// scope, and the address groups into nodes and edges under a key.
export function buildModel(
	rollups: RollupByDecision,
	workloads: Workload[],
	addressGroups: AddressGroup[],
	key: string,
	gaps: EvidenceGap[] = [],
): MapModel {
	const nodes = new Map<string, MapNode>();
	const members = new Map<string, Set<string>>();
	const listed = new Set(workloads.map((w) => w.id));
	const groupsById = new Map(addressGroups.map((g) => [g.id, g]));

	const node = (id: string, init: () => Omit<MapNode, "id">): MapNode => {
		let n = nodes.get(id);
		if (!n) {
			n = { id, ...init() };
			nodes.set(id, n);
			members.set(id, new Set());
		}
		return n;
	};
	const managed = (id: string): MapNode =>
		node(id, () => {
			const blank = {
				workloadIds: [],
				modes: {},
				unlisted: 0,
				addresses: [],
				unrecognized: [],
			};
			if (id === unlabeledId)
				return { ...blank, kind: "unlabeled", title: "unlabeled" };
			if (id === keylessId)
				return { ...blank, kind: "keyless", title: `no ${key} label` };
			const value = id.slice(2);
			return {
				...blank,
				kind: "managed",
				title: value,
				selector: `${key}=${value}`,
			};
		});
	const joinWorkload = (id: string, workloadId: string) => {
		members.get(id)?.add(workloadId);
	};

	const edges = new Map<string, MapEdge>();
	const dsts = new Set<string>();
	let connections = 0;
	let truncated = false;
	let effectiveFrom: string | null = null;
	let effectiveTo: string | null = null;

	for (const verdict of precedence) {
		const r = rollups[verdict];
		if (!r) continue;
		connections += r.totals.connection_count;
		truncated ||= r.truncated;
		if (r.effective_from)
			effectiveFrom =
				effectiveFrom === null ||
				Date.parse(r.effective_from) < Date.parse(effectiveFrom)
					? r.effective_from
					: effectiveFrom;
		if (r.effective_to)
			effectiveTo =
				effectiveTo === null
					? r.effective_to
					: later(r.effective_to, effectiveTo);
		for (const g of r.groups) {
			const pair = toPair(g, verdict);
			if (!pair) continue;
			dsts.add(pair.dst.id);

			const target = groupOf(pair.dst.labels, key);
			managed(target);
			joinWorkload(target, pair.dst.id);

			const source = peerNodeId(pair.src, key);
			if (pair.src.kind === "workload") {
				managed(source);
				if (pair.src.workload_id) joinWorkload(source, pair.src.workload_id);
			} else if (pair.src.kind === "group") {
				const ag = groupsById.get(pair.src.address_group_id ?? "");
				node(source, () => ({
					kind: "address-group",
					title: pair.src.name ?? ag?.name ?? "address group",
					addressGroupId: pair.src.address_group_id,
					cidrs: ag?.cidrs ?? [],
					workloadIds: [],
					modes: {},
					unlisted: 0,
					addresses: [],
					unrecognized: [],
				}));
			} else {
				const n = node(unknownId, () => ({
					kind: "unknown",
					title: "unknown peers",
					workloadIds: [],
					modes: {},
					unlisted: 0,
					addresses: [],
					unrecognized: [],
				}));
				const list = pair.src.kind === "address" ? n.addresses : n.unrecognized;
				if (!list.includes(pair.peerKey)) list.push(pair.peerKey);
			}

			const id = `${source}>${target}`;
			let e = edges.get(id);
			if (!e) {
				e = {
					id,
					source,
					target,
					decision: verdict,
					byDecision: {},
					connections: 0,
					lastSeen: pair.lastSeen,
					pairs: [],
				};
				edges.set(id, e);
			}
			let d = e.byDecision[verdict];
			if (!d) {
				d = { connections: 0, flows: 0, lastSeen: pair.lastSeen };
				e.byDecision[verdict] = d;
			}
			d.connections += pair.connections;
			d.flows += pair.flows;
			d.lastSeen = later(pair.lastSeen, d.lastSeen);
			e.connections += pair.connections;
			e.lastSeen = later(pair.lastSeen, e.lastSeen);
			e.pairs.push(pair);
		}
	}

	// Listed workloads join the groups the map draws, so a group counts
	// its members in scope and knows their modes even when only some of
	// them reported traffic.
	for (const w of workloads) {
		const id = groupOf(w.labels, key);
		const n = nodes.get(id);
		if (!n) continue;
		joinWorkload(id, w.id);
		n.modes[w.mode] = (n.modes[w.mode] ?? 0) + 1;
	}
	for (const [id, set] of members) {
		const n = nodes.get(id);
		if (!n) continue;
		n.workloadIds = [...set].sort();
		n.unlisted = n.workloadIds.filter((w) => !listed.has(w)).length;
		n.addresses.sort();
		n.unrecognized.sort();
	}

	for (const e of edges.values()) {
		e.decision = precedence.find((v) => e.byDecision[v]) ?? "observed";
		e.pairs.sort(
			(a, b) =>
				b.connections - a.connections ||
				precedence.indexOf(a.verdict) - precedence.indexOf(b.verdict),
		);
	}

	return {
		nodes: [...nodes.values()],
		edges: [...edges.values()],
		connections,
		reporting: dsts.size,
		truncated,
		gapped: workloadsWith(gaps),
		gaps,
		dropped: workloads.filter(
			(w) =>
				w.health.dropped_flow_records > 0 &&
				!gaps.some((g) => g.workload.id === w.id),
		),
		effectiveFrom,
		effectiveTo,
	};
}

function toPair(g: RollupGroup, verdict: Verdict): Pair | null {
	const src = g.keys.src;
	const dst = g.keys.dst;
	if (!src || !dst) return null;
	return {
		src,
		dst,
		peerKey: peerKey(src),
		verdict,
		flows: g.flow_count,
		connections: g.connection_count,
		bytes: g.byte_count,
		firstSeen: g.first_seen,
		lastSeen: g.last_seen,
	};
}

// groupingKeys are the label keys an operator can group by: every key
// carried by a workload the rollups name, in name order.
export function groupingKeys(
	rollups: RollupByDecision,
	workloads: Workload[],
): string[] {
	const keys = new Set<string>();
	const add = (l: LabelMap) => {
		for (const k of Object.keys(l)) keys.add(k);
	};
	for (const w of workloads) add(w.labels);
	for (const r of Object.values(rollups)) {
		for (const g of r?.groups ?? []) {
			if (g.keys.dst) add(g.keys.dst.labels);
			if (g.keys.src?.kind === "workload") add(g.keys.src.labels);
		}
	}
	return [...keys].sort();
}

// defaultKey is the grouping the map opens with when none is chosen:
// `app` when any workload carries it, otherwise the key carried by the
// most workloads among those that split them into more than one group,
// so the first view is groups rather than one node or one per host.
export function defaultKey(workloads: Workload[], keys: string[]): string {
	if (keys.includes("app")) return "app";
	const coverage = new Map<string, { n: number; values: Set<string> }>();
	for (const w of workloads) {
		for (const [k, v] of Object.entries(w.labels)) {
			const c = coverage.get(k) ?? { n: 0, values: new Set<string>() };
			c.n += 1;
			c.values.add(v);
			coverage.set(k, c);
		}
	}
	const ranked = keys
		.map((k) => ({ k, c: coverage.get(k) }))
		.filter((x) => (x.c?.values.size ?? 0) > 1)
		.sort((a, b) => (b.c?.n ?? 0) - (a.c?.n ?? 0) || a.k.localeCompare(b.k));
	return ranked[0]?.k ?? keys[0] ?? "app";
}

// Edge width encodes volume (tokens.css, edge-width-*): log-scaled
// across the edges in view, from --edge-width-min for none to
// --edge-width-max for the busiest edge on the map. Decision rides on
// color and dash, and selection on dimming everything else, so width
// carries volume alone.
export function volumeScale(connections: number, max: number): number {
	if (max <= 0) return 0;
	const t = Math.log1p(Math.max(connections, 0)) / Math.log1p(max);
	return Math.min(1, Math.max(0, t));
}

export function strokeWidth(
	e: Pick<MapEdge, "connections">,
	max: number,
): string {
	const t = volumeScale(e.connections, max);
	return `calc(var(--edge-width-min) + (var(--edge-width-max) - var(--edge-width-min)) * ${t.toFixed(4)})`;
}

// Dash geometry travels with decision (tokens.css, edge-dash-*):
// allowed solid, would-block "6 4", blocked "8 3 2 3", observed "1 4"
// drawn with round caps.
export const edgeDash: Record<Verdict, string> = {
	observed: "var(--edge-dash-observed)",
	allowed: "var(--edge-dash-allowed)",
	would_block: "var(--edge-dash-would-block)",
	blocked: "var(--edge-dash-blocked)",
};

export const edgeCap: Record<Verdict, "round" | "butt"> = {
	observed: "round",
	allowed: "butt",
	would_block: "butt",
	blocked: "butt",
};

export const edgeColor: Record<Verdict, string> = {
	observed: "var(--flow-observed)",
	allowed: "var(--flow-allowed)",
	would_block: "var(--flow-would-block)",
	blocked: "var(--flow-blocked)",
};

// Selection is what the operator picked: an edge or a node, by id.
export type Selection =
	| { kind: "edge"; id: string }
	| { kind: "node"; id: string }
	| null;

export function parseSelection(s: string | null): Selection {
	if (!s) return null;
	if (s.startsWith("e:")) return { kind: "edge", id: s.slice(2) };
	if (s.startsWith("n:")) return { kind: "node", id: s.slice(2) };
	return null;
}

export function formatSelection(s: Selection): string | null {
	if (!s) return null;
	return `${s.kind === "edge" ? "e" : "n"}:${s.id}`;
}

// resolveSelection keeps a selection only while the model still holds
// what it names: after a regrouping or a new range, a vanished edge or
// node is no longer selected rather than pointing at nothing.
export function resolveSelection(model: MapModel, s: Selection): Selection {
	if (!s) return null;
	if (s.kind === "edge")
		return model.edges.some((e) => e.id === s.id) ? s : null;
	return model.nodes.some((n) => n.id === s.id) ? s : null;
}

// Emphasis is how selection draws an edge: everything not in the
// current selection fades to --opacity-dimmed. An edge selection keeps
// the selected edge and its two ends; a node selection scopes the map
// to the node's own edges and neighbours.
export function edgeEmphasis(
	e: MapEdge,
	s: Selection,
): { selected: boolean; dimmed: boolean } {
	if (!s) return { selected: false, dimmed: false };
	if (s.kind === "edge") {
		const selected = s.id === e.id;
		return { selected, dimmed: !selected };
	}
	const touches = e.source === s.id || e.target === s.id;
	return { selected: false, dimmed: !touches };
}

export function nodeDimmed(
	model: MapModel,
	nodeId: string,
	s: Selection,
): boolean {
	if (s?.kind === "edge") {
		const e = model.edges.find((x) => x.id === s.id);
		return e !== undefined && e.source !== nodeId && e.target !== nodeId;
	}
	if (s?.kind !== "node" || s.id === nodeId) return false;
	return !model.edges.some(
		(e) =>
			(e.source === s.id && e.target === nodeId) ||
			(e.target === s.id && e.source === nodeId),
	);
}

// modeSummary is a managed node's mode line: the one mode its listed
// members share, or the spread when they differ.
export function modeSummary(
	n: MapNode,
): { mode: Mode } | { mixed: [Mode, number][] } | null {
	const entries = (Object.entries(n.modes) as [Mode, number][]).filter(
		([, c]) => c > 0,
	);
	if (entries.length === 0) return null;
	if (entries.length === 1) return { mode: entries[0][0] };
	const order: Mode[] = ["enforced", "simulation", "visibility"];
	return {
		mixed: entries.sort((a, b) => order.indexOf(a[0]) - order.indexOf(b[0])),
	};
}

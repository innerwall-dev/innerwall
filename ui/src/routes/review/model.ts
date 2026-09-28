import type {
	AddressGroup,
	LabelMap,
	PeerRef,
	Rollup,
	Selector,
	Workload,
	WorkloadRef,
} from "@/api/schema";
import { extent } from "@/components/RangeControl";
import { since } from "@/lib/format";
import { peerKey } from "../map/model";

// The simulation review's model: a ruleset's scope, the peer-and-service
// rollups of its workloads' traffic under the two decisions a simulating
// workload records, and the workloads themselves. Every count is the
// rollup's own or a sum of its groups; nothing is recomputed from raw
// flows, and the peer of every row is what ingestion stored.

// The decisions a simulating workload records. Visibility workloads only
// observe and enforced ones block, so neither says what enforcing a
// simulating scope would change.
export type ReviewVerdict = "would_block" | "allowed";
export const reviewVerdicts: readonly ReviewVerdict[] = [
	"would_block",
	"allowed",
];
export type Filter = "all" | ReviewVerdict;

// A rollup of each grouping the review reads, one per decision: the
// groupings carry no decision of their own.
export type ByVerdict<T> = Record<ReviewVerdict, T>;

// --- scope -------------------------------------------------------------------

// scopeRequirements is a selector as the surface's repeated `label`
// parameter takes it: one `key=value` per value, which ANDs keys and ORs
// a key's values, exactly the selector's own meaning.
export function scopeRequirements(sel: Selector): string[] {
	return Object.keys(sel)
		.sort()
		.flatMap((k) => (sel[k] ?? []).map((v) => `${k}=${v}`));
}

// scopeText is a selector as the review prints it: `app=checkout AND
// env=prod`, a key with several values in parentheses.
export function scopeText(sel: Selector): string {
	return Object.keys(sel)
		.sort()
		.map((k) => {
			const vs = sel[k] ?? [];
			return vs.length === 1
				? `${k}=${vs[0]}`
				: `(${vs.map((v) => `${k}=${v}`).join(" OR ")})`;
		})
		.join(" AND ");
}

// --- peers -------------------------------------------------------------------

// A peer group is what a row names as its peer. Workload peers fold by
// their whole label set: no selector can tell two workloads with the
// same labels apart, so no rule treats them differently. Peers that are
// not workloads keep the identity ingestion resolved them to.
export type PeerKind =
	| "workloads"
	| "unlabeled"
	| "group"
	| "address"
	| "unrecognized";

export interface PeerGroup {
	id: string;
	kind: PeerKind;
	// title leads with the group's app label (the key the flow map groups
	// by first) or its first label; rest is the remainder of the set, and
	// full the whole of it.
	title: string;
	rest?: string;
	full?: string;
	labels: LabelMap;
	addressGroupId?: string;
	cidrs?: string[];
}

function labelSet(labels: LabelMap): string {
	return Object.keys(labels)
		.sort()
		.map((k) => `${k}=${labels[k]}`)
		.join(" ");
}

function leadKey(labels: LabelMap): string {
	return "app" in labels ? "app" : (Object.keys(labels).sort()[0] ?? "");
}

export function peerGroupOf(
	p: PeerRef,
	groups: ReadonlyMap<string, AddressGroup>,
): PeerGroup {
	switch (p.kind) {
		case "workload": {
			const set = labelSet(p.labels);
			if (set) {
				const lead = leadKey(p.labels);
				const { [lead]: _, ...others } = p.labels;
				return {
					id: `w:${set}`,
					kind: "workloads",
					title: `${lead}=${p.labels[lead]}`,
					rest: labelSet(others),
					full: set,
					labels: p.labels,
				};
			}
			return {
				id: "unlabeled",
				kind: "unlabeled",
				title: "unlabeled workloads",
				labels: {},
			};
		}
		case "group": {
			const id = p.address_group_id ?? "";
			const g = groups.get(id);
			return {
				id: `ag:${id}`,
				kind: "group",
				title: p.name ?? g?.name ?? "address group",
				labels: {},
				addressGroupId: id,
				cidrs: g?.cidrs,
			};
		}
		case "address":
			return {
				id: `a:${p.address ?? ""}`,
				kind: "address",
				title: p.address ?? "",
				labels: {},
			};
		default:
			return {
				id: `u:${p.address ?? ""}`,
				kind: "unrecognized",
				title: p.address ?? "",
				labels: {},
			};
	}
}

// --- rows --------------------------------------------------------------------

// A member is one stored peer folded into a row, with what it alone did.
export interface Member {
	peer: PeerRef;
	key: string;
	workloads: number;
	connections: number;
}

// ReviewRow is one peer group reaching one service under one decision.
// workloads is how many workloads in scope saw it: exact when one peer
// makes the row, when the group's destinations can be attributed to this
// service alone, or when it reaches the whole scope; otherwise the
// largest member's count, a lower bound.
export interface ReviewRow {
	id: string;
	verdict: ReviewVerdict;
	peer: PeerGroup;
	service: string;
	members: Member[];
	flows: number;
	connections: number;
	bytes: number;
	firstSeen: string;
	lastSeen: string;
	workloads: number;
	workloadsExact: boolean;
}

export interface ReviewRollups {
	peerService: ByVerdict<Rollup>;
	srcDst: ByVerdict<Rollup>;
}

export function serviceName(s: { protocol: string; port: number }): string {
	return s.port === 0 ? s.protocol : `${s.protocol}/${s.port}`;
}

export function rowId(
	verdict: ReviewVerdict,
	peer: string,
	service: string,
): string {
	return `${verdict}|${peer}|${service}`;
}

function later(a: string, b: string): string {
	return Date.parse(a) >= Date.parse(b) ? a : b;
}
function earlier(a: string, b: string): string {
	return Date.parse(a) <= Date.parse(b) ? a : b;
}

// buildRows folds each decision's peer-and-service groups into rows.
// scopeSize is how many workloads the scope holds, the most any row can
// have reached.
export function buildRows(
	rollups: ReviewRollups,
	addressGroups: AddressGroup[],
	scopeSize: number,
): ReviewRow[] {
	const groups = new Map(addressGroups.map((g) => [g.id, g]));
	const rows: ReviewRow[] = [];
	for (const verdict of reviewVerdicts) {
		const byId = new Map<string, ReviewRow>();
		for (const g of rollups.peerService[verdict].groups) {
			const peer = g.keys.peer;
			const svc = g.keys.service;
			if (!peer || !svc) continue;
			const pg = peerGroupOf(peer, groups);
			const service = serviceName(svc);
			const id = rowId(verdict, pg.id, service);
			let row = byId.get(id);
			if (!row) {
				row = {
					id,
					verdict,
					peer: pg,
					service,
					members: [],
					flows: 0,
					connections: 0,
					bytes: 0,
					firstSeen: g.first_seen,
					lastSeen: g.last_seen,
					workloads: 0,
					workloadsExact: false,
				};
				byId.set(id, row);
			}
			row.members.push({
				peer,
				key: peerKey(peer),
				workloads: g.workload_count ?? 0,
				connections: g.connection_count,
			});
			row.flows += g.flow_count;
			row.connections += g.connection_count;
			row.bytes += g.byte_count;
			row.firstSeen = earlier(row.firstSeen, g.first_seen);
			row.lastSeen = later(row.lastSeen, g.last_seen);
		}

		// The distinct destinations of each peer group under this
		// decision, from the source-by-destination rollup. They belong to a
		// row outright when the group reached only that one service.
		const destinations = new Map<string, Set<string>>();
		for (const g of rollups.srcDst[verdict].groups) {
			const src = g.keys.src;
			const dst = g.keys.dst;
			if (!src || !dst) continue;
			const id = peerGroupOf(src, groups).id;
			let set = destinations.get(id);
			if (!set) {
				set = new Set();
				destinations.set(id, set);
			}
			set.add(dst.id);
		}
		const servicesOf = new Map<string, number>();
		for (const row of byId.values()) {
			servicesOf.set(row.peer.id, (servicesOf.get(row.peer.id) ?? 0) + 1);
		}
		const whole =
			!rollups.peerService[verdict].truncated &&
			!rollups.srcDst[verdict].truncated;
		for (const row of byId.values()) {
			const largest = Math.max(0, ...row.members.map((m) => m.workloads));
			if (row.members.length === 1) {
				row.workloads = largest;
				row.workloadsExact = true;
			} else if (
				whole &&
				servicesOf.get(row.peer.id) === 1 &&
				destinations.has(row.peer.id)
			) {
				row.workloads = destinations.get(row.peer.id)?.size ?? largest;
				row.workloadsExact = true;
			} else {
				row.workloads = largest;
				row.workloadsExact = largest >= scopeSize && scopeSize > 0;
			}
			rows.push(row);
		}
	}
	return sortRows(rows);
}

// sortRows puts what enforcing would drop first, then the busiest.
export function sortRows(rows: ReviewRow[]): ReviewRow[] {
	const rank = (v: ReviewVerdict) => reviewVerdicts.indexOf(v);
	return [...rows].sort(
		(a, b) =>
			rank(a.verdict) - rank(b.verdict) ||
			b.connections - a.connections ||
			a.peer.title.localeCompare(b.peer.title) ||
			a.service.localeCompare(b.service),
	);
}

// --- filtering and the takes ----------------------------------------------

export function filterRows(rows: ReviewRow[], filter: Filter): ReviewRow[] {
	return filter === "all" ? rows : rows.filter((r) => r.verdict === filter);
}

export function chipCounts(rows: ReviewRow[]): Record<Filter, number> {
	return {
		all: rows.length,
		would_block: rows.filter((r) => r.verdict === "would_block").length,
		allowed: rows.filter((r) => r.verdict === "allowed").length,
	};
}

// barWidth is a row's connection volume as a share of the busiest row,
// on a log scale so a scrape of thousands and a login of three both
// read, and never thinner than a sliver.
export function barWidth(connections: number, busiest: number): number {
	if (busiest <= 0) return 0;
	return Math.max(
		3,
		Math.round((Math.log(connections + 1) / Math.log(busiest + 1)) * 100),
	);
}

function servicePort(s: string): [string, number] {
	const [proto, port] = s.split("/");
	return [proto ?? s, port ? Number(port) : 0];
}

export function compareServices(a: string, b: string): number {
	const [pa, na] = servicePort(a);
	const [pb, nb] = servicePort(b);
	return pa.localeCompare(pb) || na - nb;
}

// MatrixCell is one peer and service: the rows there, the one it is
// drawn as (what enforcing would drop outranks what it admits), and the
// rest.
export interface MatrixCell {
	row: ReviewRow;
	others: ReviewRow[];
}

export interface Matrix {
	peers: PeerGroup[];
	services: string[];
	cells: Map<string, MatrixCell>;
}

export function cellKey(peer: string, service: string): string {
	return `${peer}|${service}`;
}

// matrixOf lays the rows out as peers down, services across. Peers keep
// the rows' order, so the peers with traffic enforcing would drop lead.
export function matrixOf(rows: ReviewRow[]): Matrix {
	const peers: PeerGroup[] = [];
	const seen = new Set<string>();
	const services = new Set<string>();
	const cells = new Map<string, MatrixCell>();
	for (const r of rows) {
		if (!seen.has(r.peer.id)) {
			seen.add(r.peer.id);
			peers.push(r.peer);
		}
		services.add(r.service);
		const k = cellKey(r.peer.id, r.service);
		const cell = cells.get(k);
		if (!cell) cells.set(k, { row: r, others: [] });
		else cell.others.push(r);
	}
	return { peers, services: [...services].sort(compareServices), cells };
}

export const buckets = [
	"last minute",
	"last hour",
	"last day",
	"older than a day",
] as const;
export type Bucket = (typeof buckets)[number];

export function bucketOf(lastSeen: string, now: number): Bucket {
	const age = now - Date.parse(lastSeen);
	if (age < 60_000) return "last minute";
	if (age < 3_600_000) return "last hour";
	if (age < 86_400_000) return "last day";
	return "older than a day";
}

// recencyOf buckets the rows by when each was last seen, newest first
// within a bucket: fresh traffic enforcement would drop is the strongest
// sign it would break something live.
export function recencyOf(
	rows: ReviewRow[],
	now: number,
): { bucket: Bucket; rows: ReviewRow[] }[] {
	return buckets.map((bucket) => ({
		bucket,
		rows: rows
			.filter((r) => bucketOf(r.lastSeen, now) === bucket)
			.sort((a, b) => Date.parse(b.lastSeen) - Date.parse(a.lastSeen)),
	}));
}

// resolveSelection is the selected row, if it still exists.
export function resolveSelection(
	rows: ReviewRow[],
	sel: string | null,
): ReviewRow | null {
	if (!sel) return null;
	return rows.find((r) => r.id === sel) ?? null;
}

// --- the verdict -------------------------------------------------------------

// A condition is one requirement of a safe verdict that the scope fails.
// Each has its own line, except the would-block traffic the headline
// states.
export type ConditionId =
	| "would-block"
	| "no-workloads"
	| "degraded"
	| "offline"
	| "pending"
	| "visibility"
	| "nothing-simulating";

export interface Condition {
	id: ConditionId;
	text: string;
}

export interface VerdictKpis {
	pairs: number;
	connections: number;
	// Distinct workloads in scope with traffic enforcing would drop; a
	// lower bound when the rollup it is read from was truncated.
	affected: number;
	affectedLowerBound: boolean;
	simulating: number;
	allowedPairs: number;
	// Would-block rows seen in the last hour.
	recent: number;
}

export interface ReviewVerdictResult {
	safe: boolean;
	headline: string;
	sub: string;
	failing: Condition[];
	// The lines under the headline: every failing condition but the
	// would-block traffic, then what qualifies the verdict without
	// failing it.
	caveats: string[];
	kpis: VerdictKpis;
}

export interface VerdictInput {
	rows: ReviewRow[];
	workloads: Workload[];
	rollups: ReviewRollups;
	now: number;
	rowLimit: number;
}

function plural(n: number, one: string, many: string): string {
	return n === 1 ? one : many;
}

// duration is a span in its two largest units: 14d, 1h 5m, 12m.
export function duration(ms: number): string {
	const m = Math.floor(ms / 60_000);
	const d = Math.floor(m / 1440);
	const h = Math.floor((m % 1440) / 60);
	const mm = m % 60;
	if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
	if (h > 0) return mm > 0 ? `${h}h ${mm}m` : `${h}h`;
	return `${mm}m`;
}

type SyncIssue = "degraded" | "offline" | "pending";

// syncIssue is how a workload falls short of running its latest policy:
// the agent refused it, cannot be reached, or has not applied it yet.
export function syncIssue(w: Workload): SyncIssue | null {
	if (w.sync.state === "degraded") return "degraded";
	if (w.sync.state === "offline") return "offline";
	if (
		w.sync.state === "pending" ||
		w.sync.applied_version !== w.sync.latest_version
	)
		return "pending";
	return null;
}

// composeVerdict decides whether the scope is safe to enforce. It is
// composed, never fetched: safe means no traffic in the range would be
// dropped, and every workload in scope simulates on the policy it was
// last rendered, so the verdict speaks for all of them. Each condition
// the scope fails is stated on its own.
export function composeVerdict(input: VerdictInput): ReviewVerdictResult {
	const { rows, workloads, rollups, now } = input;
	const wb = rows.filter((r) => r.verdict === "would_block");
	const allowed = rows.filter((r) => r.verdict === "allowed");
	const byMode = (m: Workload["mode"]) => workloads.filter((w) => w.mode === m);
	const simulating = byMode("simulation").length;
	const visibility = byMode("visibility").length;
	const enforced = byMode("enforced").length;

	const affectedIds = new Set(
		rollups.srcDst.would_block.groups.flatMap((g) =>
			g.keys.dst ? [g.keys.dst.id] : [],
		),
	);
	const kpis: VerdictKpis = {
		pairs: wb.length,
		connections: rollups.peerService.would_block.totals.connection_count,
		affected: affectedIds.size,
		affectedLowerBound: rollups.srcDst.would_block.truncated,
		simulating,
		allowedPairs: allowed.length,
		recent: wb.filter((r) => now - Date.parse(r.lastSeen) < 3_600_000).length,
	};

	const failing: Condition[] = [];
	if (wb.length > 0) {
		failing.push({
			id: "would-block",
			text: `${wb.length} peer/service ${plural(wb.length, "pair", "pairs")} would be dropped`,
		});
	}
	if (workloads.length === 0) {
		failing.push({
			id: "no-workloads",
			text: "No workload matches this ruleset's scope",
		});
	}
	const issues = (kind: SyncIssue) =>
		workloads.filter((w) => syncIssue(w) === kind);
	const degraded = issues("degraded");
	if (degraded.length > 0) {
		failing.push({
			id: "degraded",
			text:
				degraded.length === 1
					? `${degraded[0].hostname} is degraded (applied v${degraded[0].sync.applied_version}, rendered v${degraded[0].sync.latest_version}) — its flows were evaluated against stale policy`
					: `${degraded.length} workloads in scope are degraded — their flows were evaluated against stale policy`,
		});
	}
	const offline = issues("offline");
	if (offline.length > 0) {
		const seen = offline[0].health.last_seen_at;
		failing.push({
			id: "offline",
			text:
				offline.length === 1
					? `${offline[0].hostname} is offline${seen ? ` (last seen ${since(seen, now)} ago)` : ""} — nothing it saw since reaches this review`
					: `${offline.length} workloads in scope are offline — nothing they saw since reaches this review`,
		});
	}
	const pending = issues("pending");
	if (pending.length > 0) {
		failing.push({
			id: "pending",
			text:
				pending.length === 1
					? `${pending[0].hostname} has not applied its latest policy (applied v${pending[0].sync.applied_version}, rendered v${pending[0].sync.latest_version})`
					: `${pending.length} workloads in scope have not applied their latest policy`,
		});
	}
	if (visibility > 0) {
		failing.push({
			id: "visibility",
			text: `${visibility} ${plural(visibility, "workload in scope is", "workloads in scope are")} still in visibility mode and produced no verdict`,
		});
	}
	if (workloads.length > 0 && simulating === 0 && visibility === 0) {
		failing.push({
			id: "nothing-simulating",
			text: `All ${enforced} ${plural(enforced, "workload", "workloads")} in scope ${plural(enforced, "is", "are")} already enforced`,
		});
	}
	const safe = failing.length === 0;

	const notes: string[] = [];
	const droppers = workloads.filter((w) => w.health.dropped_flow_records > 0);
	if (droppers.length === 1) {
		notes.push(
			`${droppers[0].hostname} dropped ${droppers[0].health.dropped_flow_records.toLocaleString("en-US")} flow records — its verdict may be incomplete`,
		);
	} else if (droppers.length > 1) {
		notes.push(
			`${droppers.length} workloads dropped flow records — their verdicts may be incomplete`,
		);
	}
	const truncated = reviewVerdicts.some(
		(v) => rollups.peerService[v].truncated,
	);
	if (truncated) {
		notes.push(
			`Only the busiest ${input.rowLimit.toLocaleString("en-US")} peer/service pairs per decision are listed; the totals count every pair`,
		);
	}
	if (safe) {
		const from = earliestEffective(rollups);
		const to = latestEffective(rollups);
		notes.push(
			from && to
				? `Observed for ${duration(Date.parse(to) - Date.parse(from))} (${extent(from, to)}); a peer that connects less often than that has not been seen`
				: "No flows were stored in this range; no observed traffic stands behind this verdict",
		);
	}

	const caveats = [
		...failing.filter((c) => c.id !== "would-block").map((c) => c.text),
		...notes,
	];

	let headline: string;
	let sub: string;
	if (safe) {
		headline = "Safe to enforce";
		sub = `Every observed inbound connection on the ${simulating} simulating ${plural(simulating, "workload", "workloads")} matched an enabled rule. Enforcing changes nothing for the traffic seen in this range.`;
	} else if (workloads.length === 0) {
		headline = "No workload in scope";
		sub =
			"This ruleset's scope matches no workload, so there is nothing to enforce.";
	} else if (failing.some((c) => c.id === "nothing-simulating")) {
		headline = "Nothing is simulating in this scope";
		sub =
			"Every workload in scope is already enforced; there is no simulated verdict to review.";
	} else if (wb.length > 0) {
		headline = "Not safe to enforce yet";
		sub = `${wb.length} peer/service ${plural(wb.length, "pair", "pairs")} carrying ${kpis.connections.toLocaleString("en-US")} connections would be dropped. ${kpis.recent} of them ${plural(kpis.recent, "was", "were")} seen in the last hour.`;
	} else {
		headline = "Not safe to enforce yet";
		sub =
			"No observed traffic would be dropped, but not every workload in scope is simulating on its latest policy.";
	}
	return { safe, headline, sub, failing, caveats, kpis };
}

// earliestEffective and latestEffective are the widest windows any of
// the review's rollups counted.
export function earliestEffective(r: ReviewRollups): string | null {
	const all = reviewVerdicts
		.flatMap((v) => [r.peerService[v].effective_from])
		.filter((x): x is string => x !== null);
	return all.length === 0 ? null : all.reduce(earlier);
}

export function latestEffective(r: ReviewRollups): string | null {
	const all = reviewVerdicts
		.flatMap((v) => [r.peerService[v].effective_to])
		.filter((x): x is string => x !== null);
	return all.length === 0 ? null : all.reduce(later);
}

// --- promotion -----------------------------------------------------------------

// A promotion changes the mode of the workloads that earned it: those
// simulating on their latest policy. Workloads in visibility never
// simulated, so there is no evidence to enforce them on (ADR-0001), and
// they are never offered. A simulating workload that is not running its
// latest policy is left out unless the operator includes it, knowing it
// would enforce what it last applied.
export interface Partition {
	included: Workload[];
	unsynced: { workload: Workload; issue: SyncIssue }[];
	visibility: Workload[];
	enforced: Workload[];
	// Workloads the scope matched that the listing did not return: the
	// fleet moved between the two reads.
	unlisted: WorkloadRef[];
}

export function partition(
	matched: WorkloadRef[],
	workloads: Workload[],
): Partition {
	const byId = new Map(workloads.map((w) => [w.id, w]));
	const out: Partition = {
		included: [],
		unsynced: [],
		visibility: [],
		enforced: [],
		unlisted: [],
	};
	for (const ref of matched) {
		const w = byId.get(ref.id);
		if (!w) {
			out.unlisted.push(ref);
			continue;
		}
		if (w.mode === "visibility") out.visibility.push(w);
		else if (w.mode === "enforced") out.enforced.push(w);
		else {
			const issue = syncIssue(w);
			if (issue) out.unsynced.push({ workload: w, issue });
			else out.included.push(w);
		}
	}
	return out;
}

// promotionIds is the set a promotion submits: the included workloads
// and the unsynced ones the operator chose to include, by id.
export function promotionIds(
	p: Partition,
	optedIn: ReadonlySet<string>,
): string[] {
	return [
		...p.included.map((w) => w.id),
		...p.unsynced
			.filter((u) => optedIn.has(u.workload.id))
			.map((u) => u.workload.id),
	];
}

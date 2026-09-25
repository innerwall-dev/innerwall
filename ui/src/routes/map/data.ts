import { getRollup, listAddressGroups, listWorkloads } from "@/api/fleet";
import type { AddressGroup, Verdict, Workload } from "@/api/schema";
import type { RollupByDecision } from "./model";
import { precedence } from "./model";

// The time ranges the map offers. Stored windows are what the rollup
// counts, so a range covers the windows that lie inside it and its
// honest extent is the rollup's effective bounds, not the range asked.
export const ranges = {
	"1h": 3_600_000,
	"6h": 6 * 3_600_000,
	"24h": 24 * 3_600_000,
	"7d": 7 * 24 * 3_600_000,
	"30d": 30 * 24 * 3_600_000,
} as const;
export type RangeKey = keyof typeof ranges;
export const defaultRange: RangeKey = "24h";

export function isRange(s: string | null): s is RangeKey {
	return s !== null && Object.hasOwn(ranges, s);
}

// The most groups one rollup returns, and the page size of the walk of
// the workloads in scope: each the surface's maximum.
export const rollupLimit = 1000;
export const workloadPage = 500;

export interface MapData {
	from: string;
	to: string;
	rollups: RollupByDecision;
	workloads: Workload[];
	addressGroups: AddressGroup[];
}

// loadMap reads what the map draws, over one fixed range so every read
// agrees: the source-by-destination rollup once per decision (the
// grouping carries no decision of its own), every workload in scope
// (their modes, their members, and their dropped-record counters, which
// only the workload carries), and the address groups (their CIDRs).
export async function loadMap(
	scope: readonly string[],
	range: RangeKey,
	now = Date.now(),
): Promise<MapData> {
	const to = new Date(now).toISOString();
	const from = new Date(now - ranges[range]).toISOString();
	const label = scope.length > 0 ? scope : undefined;
	const [rollups, workloads, groups] = await Promise.all([
		Promise.all(
			precedence.map((verdict: Verdict) =>
				getRollup({
					group_by: "src,dst",
					from,
					to,
					verdict,
					label,
					limit: rollupLimit,
				}).then((r) => [verdict, r] as const),
			),
		).then((pairs) => Object.fromEntries(pairs) as RollupByDecision),
		walkWorkloads(scope),
		listAddressGroups(),
	]);
	return { from, to, rollups, workloads, addressGroups: groups.address_groups };
}

// walkWorkloads reads every workload the scope selects, a page at a
// time, to the end.
async function walkWorkloads(scope: readonly string[]): Promise<Workload[]> {
	const all: Workload[] = [];
	let cursor: string | undefined;
	do {
		const page = await listWorkloads(
			{ label: scope.length > 0 ? scope : undefined },
			cursor,
			workloadPage,
		);
		all.push(...page.workloads);
		cursor = page.next_cursor ?? undefined;
	} while (cursor);
	return all;
}

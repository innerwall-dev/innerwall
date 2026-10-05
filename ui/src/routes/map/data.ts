import {
	getGaps,
	getRollup,
	listAddressGroups,
	listWorkloads,
} from "@/api/fleet";
import type {
	AddressGroup,
	EvidenceGaps,
	Verdict,
	Workload,
} from "@/api/schema";
import { type RangeKey, ranges } from "@/components/RangeControl";
import type { RollupByDecision } from "./model";
import { precedence } from "./model";

export {
	defaultRange,
	isRange,
	type RangeKey,
	ranges,
} from "@/components/RangeControl";

// The most groups one rollup returns, and the page size of the walk of
// the workloads in scope: each the surface's maximum.
export const rollupLimit = 1000;
export const workloadPage = 500;
// The most evidence gaps one read returns: the surface's maximum.
export const gapLimit = 5000;

export interface MapData {
	from: string;
	to: string;
	rollups: RollupByDecision;
	workloads: Workload[];
	addressGroups: AddressGroup[];
	gaps: EvidenceGaps;
}

// loadMap reads what the map draws, over one fixed range so every read
// agrees: the source-by-destination rollup once per decision (the
// grouping carries no decision of its own), every workload in scope
// (their modes, their members, and their dropped-record counters, which
// only the workload carries), the address groups (their CIDRs), and the
// evidence gaps the workloads in scope reported in the range.
export async function loadMap(
	scope: readonly string[],
	range: RangeKey,
	now = Date.now(),
): Promise<MapData> {
	const to = new Date(now).toISOString();
	const from = new Date(now - ranges[range]).toISOString();
	const label = scope.length > 0 ? scope : undefined;
	const [rollups, workloads, groups, gaps] = await Promise.all([
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
		getGaps({ label, from, to, limit: gapLimit }),
	]);
	return {
		from,
		to,
		rollups,
		workloads,
		addressGroups: groups.address_groups,
		gaps,
	};
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

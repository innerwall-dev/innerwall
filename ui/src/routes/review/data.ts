import {
	getGaps,
	getRollup,
	listAddressGroups,
	listWorkloads,
} from "@/api/fleet";
import { listRulesets } from "@/api/policy";
import type {
	AddressGroup,
	EvidenceGaps,
	Rollup,
	RollupGrouping,
	Ruleset,
	Workload,
} from "@/api/schema";
import { type RangeKey, ranges } from "@/components/RangeControl";
import {
	type ByVerdict,
	peerGroupOf,
	type ReviewRollups,
	type ReviewVerdict,
	reviewVerdicts,
	scopeRequirements,
	serviceName,
} from "./model";

// The most groups one rollup returns, and the page size of the walk of
// the workloads in scope: each the surface's maximum.
export const rowLimit = 1000;
const workloadPage = 500;
// The most evidence gaps one read returns: the surface's maximum.
export const gapLimit = 5000;

// TabCount is a ruleset's would-block peer/service pairs over the range,
// "+" when its rollup was truncated.
export interface TabCount {
	pairs: number;
	truncated: boolean;
}

export interface ReviewData {
	// requested is the ruleset name the read was asked for, null for the
	// default; ruleset is the one it read, which differs when no enabled
	// ruleset has that name.
	requested: string | null;
	from: string;
	to: string;
	// The enabled rulesets, in the listing's order; a disabled ruleset
	// renders nothing, so there is nothing of it to review.
	rulesets: Ruleset[];
	// How many rulesets exist at all, enabled or not.
	total: number;
	counts: Record<string, TabCount>;
	ruleset: Ruleset | null;
	rollups: ReviewRollups | null;
	workloads: Workload[];
	// The evidence gaps of the scope's workloads over the range.
	gaps: EvidenceGaps | null;
	addressGroups: AddressGroup[];
}

function rollup(
	group_by: RollupGrouping,
	verdict: ReviewVerdict,
	scope: string[],
	from: string,
	to: string,
): Promise<Rollup> {
	return getRollup({
		group_by,
		verdict,
		label: scope,
		from,
		to,
		limit: rowLimit,
	});
}

// pairCount is how many rows a would-block rollup folds into, the count
// a ruleset's tab shows.
function pairCount(r: Rollup, groups: ReadonlyMap<string, AddressGroup>) {
	const ids = new Set<string>();
	for (const g of r.groups) {
		if (g.keys.peer && g.keys.service) {
			ids.add(
				`${peerGroupOf(g.keys.peer, groups).id}|${serviceName(g.keys.service)}`,
			);
		}
	}
	return { pairs: ids.size, truncated: r.truncated };
}

// loadReview reads what the review shows, over one fixed range so every
// read agrees. First the rulesets and each one's would-block traffic,
// which the tabs count and which picks the ruleset to open when none is
// named (the one with the most); then, for that ruleset's scope, the
// peer-and-service and source-by-destination rollups under each
// decision, every workload the scope selects, and the evidence gaps
// they reported in the range.
export async function loadReview(
	name: string | null,
	range: RangeKey,
	now = Date.now(),
): Promise<ReviewData> {
	const to = new Date(now).toISOString();
	const from = new Date(now - ranges[range]).toISOString();
	const [listing, groupsPage] = await Promise.all([
		listRulesets(),
		listAddressGroups(),
	]);
	const addressGroups = groupsPage.address_groups;
	const groups = new Map(addressGroups.map((g) => [g.id, g]));
	const rulesets = listing.rulesets.filter((r) => r.enabled !== false);
	const empty: ReviewData = {
		requested: name,
		from,
		to,
		rulesets,
		total: listing.rulesets.length,
		counts: {},
		ruleset: null,
		rollups: null,
		workloads: [],
		gaps: null,
		addressGroups,
	};
	if (rulesets.length === 0) return empty;

	const wouldBlock = await Promise.all(
		rulesets.map((rs) =>
			rollup(
				"peer,service",
				"would_block",
				scopeRequirements(rs.scope),
				from,
				to,
			),
		),
	);
	const counts: Record<string, TabCount> = {};
	rulesets.forEach((rs, i) => {
		counts[rs.id ?? rs.name] = pairCount(wouldBlock[i], groups);
	});
	let index = rulesets.findIndex((rs) => rs.name === name);
	if (index < 0) {
		index = 0;
		rulesets.forEach((rs, i) => {
			const c = counts[rs.id ?? rs.name].pairs;
			if (c > counts[rulesets[index].id ?? rulesets[index].name].pairs)
				index = i;
		});
	}
	const ruleset = rulesets[index];
	const scope = scopeRequirements(ruleset.scope);

	const [allowed, srcDst, workloads, gaps] = await Promise.all([
		rollup("peer,service", "allowed", scope, from, to),
		Promise.all(
			reviewVerdicts.map((v) => rollup("src,dst", v, scope, from, to)),
		).then(
			(rs) =>
				Object.fromEntries(
					reviewVerdicts.map((v, i) => [v, rs[i]]),
				) as ByVerdict<Rollup>,
		),
		walkWorkloads(scope),
		getGaps({
			label: scope.length > 0 ? scope : undefined,
			from,
			to,
			limit: gapLimit,
		}),
	]);
	return {
		...empty,
		counts,
		ruleset,
		rollups: {
			peerService: { would_block: wouldBlock[index], allowed },
			srcDst,
		},
		workloads,
		gaps,
	};
}

// walkWorkloads reads every workload the scope selects, a page at a
// time, to the end.
export async function walkWorkloads(scope: string[]): Promise<Workload[]> {
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

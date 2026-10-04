import { getRollup, listAddressGroups } from "@/api/fleet";
import { listRulesets, listServices, previewSelector } from "@/api/policy";
import type { AddressGroup, Ruleset, Selector, Service } from "@/api/schema";
import { walkWorkloads } from "../review/data";
import { scopeRequirements } from "../review/model";
import {
	type RuleTraffic,
	type ScopeMatch,
	trafficByRule,
	trafficRangeMs,
} from "./model";

// The editor's reads. The rulesets and the state version come from one
// listing, which the control plane reads from one snapshot, so the
// version names exactly the set the editor authors against; the service
// definitions and address groups name what rules reference.

export interface EditorData {
	rulesets: Ruleset[];
	stateVersion: string;
	services: Service[];
	groups: AddressGroup[];
}

export async function loadEditor(): Promise<EditorData> {
	const [listing, services, groups] = await Promise.all([
		listRulesets(),
		listServices(),
		listAddressGroups(),
	]);
	return {
		rulesets: listing.rulesets,
		stateVersion: listing.state_version,
		services: services.services,
		groups: groups.address_groups,
	};
}

// loadScope is what a scope matches now: the control plane's preview
// (the renderer's own match), joined by id with the scoped walk of the
// workloads, which carries their mode and sync state. Two reads joined;
// the console counts nothing the preview did not resolve.
export async function loadScope(scope: Selector): Promise<ScopeMatch> {
	const [preview, workloads] = await Promise.all([
		previewSelector(scope),
		walkWorkloads(scopeRequirements(scope)),
	]);
	return {
		count: preview.count,
		matched: preview.matched,
		byId: new Map(workloads.map((w) => [w.id, w])),
	};
}

// The most rules one rollup reports; a truncated one says so.
const trafficLimit = 1000;

export interface Traffic {
	byRule: Map<string, RuleTraffic>;
	truncated: boolean;
}

// loadTraffic is the simulation column: the admitted traffic of the
// scope's workloads over the last day, grouped by the rule that admitted
// it, each group with the workloads that reported it.
export async function loadTraffic(
	scope: Selector,
	now = Date.now(),
): Promise<Traffic> {
	const r = await getRollup({
		group_by: "rule",
		verdict: "allowed",
		label: scopeRequirements(scope),
		from: new Date(now - trafficRangeMs).toISOString(),
		to: new Date(now).toISOString(),
		limit: trafficLimit,
	});
	return { byRule: trafficByRule(r), truncated: r.truncated };
}

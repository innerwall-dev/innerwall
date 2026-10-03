import { request } from "./client";
import type {
	Rule,
	RuleInput,
	Ruleset,
	Selector,
	SelectorPreview,
	Service,
} from "./schema";

// The policy endpoints the review reads and writes. Each function is one
// request against the public surface (ADR-0007).

export function listRulesets(): Promise<{
	rulesets: Ruleset[];
	state_version: string;
}> {
	return request("GET", "/rulesets");
}

export function listServices(): Promise<{ services: Service[] }> {
	return request("GET", "/services");
}

// previewSelector is what a selector resolves to now: the resolution a
// mode change by selector would use.
export function previewSelector(selector: Selector): Promise<SelectorPreview> {
	return request("POST", "/selectors/preview", { selector });
}

// putRule replaces one rule, conditioned on the version it was read at:
// a rule changed since is refused with its current version, never
// overwritten.
export function putRule(
	rulesetId: string,
	rule: Rule,
	change: Partial<RuleInput>,
): Promise<Rule> {
	const body: RuleInput = {
		id: rule.id,
		direction: rule.direction,
		enabled: rule.enabled,
		description: rule.description,
		peers: rule.peers,
		services: rule.services,
		entries: rule.entries,
		...change,
	};
	// Every persisted rule has an id; the generated type cannot say so.
	const id = rule.id ?? "";
	return request(
		"PUT",
		`/rulesets/${encodeURIComponent(rulesetId)}/rules/${encodeURIComponent(id)}`,
		body,
		{ "If-Match": `"${rule.version}"` },
	);
}

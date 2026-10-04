import { request } from "./client";
import type {
	DryRunRequest,
	DryRunResult,
	Rule,
	RuleInput,
	Ruleset,
	RulesetInput,
	Selector,
	SelectorPreview,
	Service,
} from "./schema";

// The policy endpoints the review and the editor read and write. Each
// function is one request against the public surface (ADR-0007). Every
// write to an existing object is conditioned on the version it was read
// at: an object changed since is refused with its current version, never
// overwritten.

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

// ruleInput is a persisted rule as the surface takes it back: the
// authored fields, without the version and instants it ignores.
export function ruleInput(rule: Rule): RuleInput {
	return {
		id: rule.id,
		direction: rule.direction,
		enabled: rule.enabled,
		description: rule.description,
		peers: rule.peers,
		services: rule.services,
		entries: rule.entries,
	};
}

// rulesetInput is a persisted ruleset as the surface takes it back.
export function rulesetInput(rs: Ruleset): RulesetInput {
	return {
		id: rs.id,
		name: rs.name,
		description: rs.description,
		enabled: rs.enabled,
		scope: rs.scope,
		rules: rs.rules.map(ruleInput),
	};
}

// putRule replaces one rule, conditioned on the version it was read at.
export function putRule(
	rulesetId: string,
	rule: Rule,
	change: Partial<RuleInput>,
): Promise<Rule> {
	const body: RuleInput = { ...ruleInput(rule), ...change };
	// Every persisted rule has an id; the generated type cannot say so.
	const id = rule.id ?? "";
	return request(
		"PUT",
		`/rulesets/${encodeURIComponent(rulesetId)}/rules/${encodeURIComponent(id)}`,
		body,
		{ "If-Match": `"${rule.version}"` },
	);
}

// createRule adds one rule to a ruleset; the control plane conditions
// the ruleset's write on the version it read itself.
export function createRule(rulesetId: string, rule: RuleInput): Promise<Rule> {
	return request(
		"POST",
		`/rulesets/${encodeURIComponent(rulesetId)}/rules`,
		rule,
	);
}

// putRuleset replaces a ruleset and its rules, conditioned on the
// ruleset's version as read; the rules go back as they were read.
export function putRuleset(
	ruleset: Ruleset,
	change: Partial<Omit<RulesetInput, "rules">>,
): Promise<Ruleset> {
	return request(
		"PUT",
		`/rulesets/${encodeURIComponent(ruleset.id ?? "")}`,
		{ ...rulesetInput(ruleset), ...change },
		{ "If-Match": `"${ruleset.version}"` },
	);
}

export function createRuleset(ruleset: RulesetInput): Promise<Ruleset> {
	return request("POST", "/rulesets", ruleset);
}

// renderDryRun renders a hypothetical policy set and reports what would
// change per workload; nothing is persisted and no version moves.
export function renderDryRun(body: DryRunRequest): Promise<DryRunResult> {
	return request("POST", "/policies/render-dryrun", body);
}

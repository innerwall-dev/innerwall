import { rulesetInput } from "@/api/policy";
import type {
	AddressGroup,
	DryRunRequest,
	DryRunResult,
	Entry,
	Finding,
	Peer,
	RenderedRuleDelta,
	Rollup,
	Rule,
	RuleInput,
	Ruleset,
	RulesetInput,
	Selector,
	Service,
	Workload,
	WorkloadRef,
} from "@/api/schema";
import { since } from "@/lib/format";
import { parseRequirements, type Requirement } from "@/lib/labels";
import { syncIssue } from "../review/model";

type SyncIssue = NonNullable<ReturnType<typeof syncIssue>>;

// The policy editor's model: rules as the table draws them, the draft an
// edited row holds, the findings of a refused write attached to what
// they name, the hypothetical set a dry run submits, and how a dry run's
// result reads. Everything here is a transform of what the surface
// returned or of what the operator typed; admission is the control
// plane's, and nothing here judges whether a rule is valid.

// --- chips -------------------------------------------------------------------

// A chip is one authored element as a cell draws it: its kind tag, its
// text, and whether it names something defined elsewhere (a reference,
// drawn dashed) or states its value inline (drawn solid).
export interface Chip {
	tag: "LABELS" | "GROUP" | "CIDR" | "REF" | "TCP" | "UDP" | "ICMP";
	text: string;
	reference: boolean;
	// requirements are a LABELS chip's selector, in key order, so each
	// draws as one label chip from the selector itself, never from text
	// re-split (a value stored before the label grammar may hold a space).
	requirements?: Requirement[];
}

// selectorText is a selector as one chip's text: its requirements in key
// order, ANDed by the space between them, a key's several values ORed
// with "|". One chip is one selector, so a rule whose peer requires two
// labels reads as one requirement of both, never as two peers.
export function selectorText(sel: Selector): string {
	return Object.keys(sel)
		.sort()
		.map((k) => `${k}=${(sel[k] ?? []).join("|")}`)
		.join(" ");
}

export interface Names {
	services: ReadonlyMap<string, string>;
	groups: ReadonlyMap<string, string>;
}

export function namesOf(
	services: readonly Service[],
	groups: readonly AddressGroup[],
): Names {
	return {
		services: new Map(services.map((s) => [s.id, s.name])),
		groups: new Map(groups.map((g) => [g.id, g.name])),
	};
}

export function peerChip(p: Peer, names: Names): Chip {
	if (p.workloads) {
		const sel = p.workloads;
		return {
			tag: "LABELS",
			text: selectorText(sel),
			reference: false,
			requirements: Object.keys(sel)
				.sort()
				.map((key) => ({ key, values: sel[key] ?? [] })),
		};
	}
	if (p.address_group !== undefined) {
		return {
			tag: "GROUP",
			text: names.groups.get(p.address_group) ?? p.address_group,
			reference: true,
		};
	}
	return { tag: "CIDR", text: p.cidr ?? "", reference: false };
}

export function entryChip(e: Entry): Chip {
	const tag = e.protocol.toUpperCase() as Chip["tag"];
	const ports = e.ports ?? [];
	return {
		tag,
		text:
			ports.length > 0
				? ports.join(", ")
				: e.protocol === "icmp"
					? ""
					: "every port",
		reference: false,
	};
}

// serviceRefChip is a reference to a named service: the surface writes
// ids, an operator types names, and either reads as the name when the
// listing has it.
export function serviceRefChip(ref: string, names: Names): Chip {
	return { tag: "REF", text: names.services.get(ref) ?? ref, reference: true };
}

// servicesCell is a rule's services as its cell draws them: the named
// services it references, then the entries it states inline. A rule
// permits the union of the two (a rule may carry both), so both are
// listed, each in its own form.
export function servicesCell(
	r: Pick<RuleInput, "services" | "entries">,
	names: Names,
): Chip[] {
	return [
		...(r.services ?? []).map((s) => serviceRefChip(s, names)),
		...(r.entries ?? []).map(entryChip),
	];
}

// --- the rule's identity and recency -----------------------------------------

// shortRuleId is the first group of a rule's id: rules have no name, so
// the table identifies them by it under their description.
export function shortRuleId(id: string): string {
	return id.split("-")[0] ?? id;
}

const recentMs = 24 * 60 * 60 * 1000;

// recency is the "recently changed" cue: a rule created or changed in the
// last day, from the instants the control plane stamped.
export function recency(r: Rule, now = Date.now()): string | null {
	if (now - Date.parse(r.created_at) < recentMs) {
		return `added ${since(r.created_at, now)} ago`;
	}
	if (now - Date.parse(r.updated_at) < recentMs) {
		return `changed ${since(r.updated_at, now)} ago`;
	}
	return null;
}

// --- the rule's traffic --------------------------------------------------------

// The editor's simulation column reads the last day.
export const trafficRangeMs = 24 * 60 * 60 * 1000;

// RuleTraffic is one authored rule's admitted traffic: the connections
// its resolved rules matched, and the workloads that reported them.
// Each protocol of a rule is its own resolved rule and its own group;
// workloads behind two of them can overlap, so for a rule with several
// the largest count is a lower bound and is said to be one.
export interface RuleTraffic {
	connections: number;
	workloads: number;
	atLeast: boolean;
}

// trafficByRule folds the rule rollup of the allowed decision into one
// entry per authored rule. A truncated rollup leaves rules out; the
// caller says so rather than reading their absence as no traffic.
export function trafficByRule(rollup: Rollup): Map<string, RuleTraffic> {
	const out = new Map<string, RuleTraffic & { groups: number }>();
	for (const g of rollup.groups) {
		const rule = g.keys.rule;
		if (!rule) continue;
		const id = rule.authored_rule_id ?? rule.id.split("/")[0] ?? rule.id;
		const cur = out.get(id) ?? {
			connections: 0,
			workloads: 0,
			atLeast: false,
			groups: 0,
		};
		cur.connections += g.connection_count;
		cur.workloads = Math.max(cur.workloads, g.workload_count ?? 0);
		cur.groups += 1;
		cur.atLeast = cur.groups > 1;
		out.set(id, cur);
	}
	return new Map([...out].map(([id, { groups: _, ...t }]) => [id, t] as const));
}

// --- the scope -----------------------------------------------------------------

// withRequirement adds or replaces one key's requirement.
export function withRequirement(
	sel: Selector,
	key: string,
	values: string[],
): Selector {
	return { ...sel, [key]: values };
}

export function withoutKey(sel: Selector, key: string): Selector {
	const { [key]: _, ...rest } = sel;
	return rest;
}

export function sameSelector(a: Selector, b: Selector): boolean {
	return selectorText(a) === selectorText(b);
}

// ScopeMatch is a scope as the editor reports it: what the control plane
// resolves it to now, joined by id with the workloads the scoped walk
// read, which carry the mode and sync state the preview does not.
export interface ScopeMatch {
	count: number;
	matched: WorkloadRef[];
	byId: ReadonlyMap<string, Workload>;
}

export interface ScopeMix {
	simulation: number;
	visibility: number;
	enforced: number;
	degraded: number;
	offline: number;
	pending: number;
}

export function scopeMix(m: ScopeMatch): ScopeMix {
	const mix: ScopeMix = {
		simulation: 0,
		visibility: 0,
		enforced: 0,
		degraded: 0,
		offline: 0,
		pending: 0,
	};
	for (const ref of m.matched) {
		const w = m.byId.get(ref.id);
		if (!w) continue;
		mix[w.mode] += 1;
		const issue = syncIssue(w);
		if (issue) mix[issue] += 1;
	}
	return mix;
}

// MatchedHost is one matched workload as the scope card lists it, with
// what keeps it from its latest policy, if anything.
export interface MatchedHost {
	id: string;
	hostname: string;
	issue: SyncIssue | null;
}

// matchedHosts lists the workloads a scope matches, those needing
// attention first, then by hostname.
export function matchedHosts(m: ScopeMatch): MatchedHost[] {
	const rank = (h: MatchedHost) => (h.issue ? 0 : 1);
	return m.matched
		.map((r) => {
			const w = m.byId.get(r.id);
			return { id: r.id, hostname: r.hostname, issue: w ? syncIssue(w) : null };
		})
		.sort((a, b) => rank(a) - rank(b) || a.hostname.localeCompare(b.hostname));
}

// bannerText is the immediate-effect statement: there is no draft, and
// what saving does to the workloads the scope holds now. It says what
// enforced workloads do only when the scope holds some.
export function bannerText(opts: {
	enabled: boolean;
	count: number | null;
	mix: ScopeMix | null;
}): string {
	const n =
		opts.count === null
			? "the workloads"
			: `the ${opts.count} ${opts.count === 1 ? "workload" : "workloads"}`;
	if (!opts.enabled) {
		return `There is no draft. This ruleset is disabled, so its rules render onto no workload; saving writes it immediately, and enabling it re-renders ${n} in scope and pushes the change to their agents at once.`;
	}
	const enforced = opts.mix?.enforced ?? 0;
	const tail =
		enforced > 0
			? ` ${enforced} enforced ${enforced === 1 ? "workload drops" : "workloads drop"} what no enabled rule admits; workloads in simulation still drop nothing.`
			: " Workloads in simulation still drop nothing.";
	return `There is no draft. Saving an enabled rule re-renders ${n} in scope and pushes the change to their agents immediately.${tail}`;
}

// --- drafts ------------------------------------------------------------------

// Each element of a draft carries a key of its own, so a finding stays
// on the element it named while the operator removes or adds others.
let serial = 0;
export function nextKey(): string {
	serial += 1;
	return `k${serial}`;
}

export interface Keyed<T> {
	key: string;
	value: T;
}

const keyed = <T>(value: T): Keyed<T> => ({ key: nextKey(), value });

// RuleDraft is one row being edited: a persisted rule (base) or a new
// one, with a temporary id a dry run carries so its deltas can be told
// apart from the persisted rules'. The temporary id names nothing
// persisted and is never sent on a save.
export interface RuleDraft {
	base: Rule | null;
	tempId: string;
	enabled: boolean;
	description: string;
	peers: Keyed<Peer>[];
	refs: Keyed<string>[];
	entries: Keyed<Entry>[];
}

export function draftOf(rule: Rule | null, tempId: string): RuleDraft {
	return {
		base: rule,
		tempId,
		enabled: rule?.enabled !== false,
		description: rule?.description ?? "",
		peers: (rule?.peers ?? []).map(keyed),
		refs: (rule?.services ?? []).map(keyed),
		entries: (rule?.entries ?? []).map(keyed),
	};
}

// draftInput is the draft as the surface takes it. A new rule has no
// id: the control plane assigns one on create. dryRunId gives it the
// draft's temporary id instead, for a dry run alone.
export function draftInput(d: RuleDraft, dryRunId = false): RuleInput {
	const id = d.base?.id ?? (dryRunId ? d.tempId : undefined);
	return {
		...(id ? { id } : {}),
		direction: "inbound",
		enabled: d.enabled,
		description: d.description,
		peers: d.peers.map((p) => p.value),
		services: d.refs.map((r) => r.value),
		entries: d.entries.map((e) => e.value),
	};
}

// sameInput says whether two drafts would write the same rule.
export function sameInput(a: RuleDraft, b: RuleDraft): boolean {
	return JSON.stringify(draftInput(a)) === JSON.stringify(draftInput(b));
}

// dirty says whether a draft differs from the rule it started from; a new
// rule is always unsaved.
export function dirty(d: RuleDraft): boolean {
	return !d.base || !sameInput(d, draftOf(d.base, d.tempId));
}

// parsePeer reads one peer as typed or pasted. Requirements (`key=value`,
// several separated by spaces, a key's values by "|") are a workload
// selector, read by the console's one label parser and refused with the
// reason when they are outside the label grammar (ADR-0022); an address
// or a prefix is a CIDR; anything else names an address group. A CIDR or
// a group name is kept as typed, so the control plane's admission names
// any fault in it.
export function parsePeer(text: string): Peer | { error: string } {
	const t = text.trim();
	if (t.includes("=")) {
		const parsed = parseRequirements(t, { alternatives: true });
		if (!parsed.ok) return { error: parsed.error };
		const sel: Selector = {};
		for (const r of parsed.requirements) {
			sel[r.key] = [...(sel[r.key] ?? []), ...r.values];
		}
		return { workloads: sel };
	}
	if (/^[0-9a-f:.]+(\/\d+)?$/i.test(t) && /[.:]/.test(t)) {
		return { cidr: t };
	}
	return { address_group: t };
}

// parseService reads one service as typed: a protocol, optionally with
// its ports after a slash (`tcp/389`, `tcp/6000-6010,6012`, `icmp`), is
// an inline entry; anything else names a service definition.
export function parseService(text: string): { entry: Entry } | { ref: string } {
	const t = text.trim();
	const m = /^(tcp|udp|icmp)(?:\/(.*))?$/i.exec(t);
	if (!m) return { ref: t };
	const protocol = (m[1] ?? "").toLowerCase() as Entry["protocol"];
	const ports = (m[2] ?? "")
		.split(",")
		.map((p) => p.trim())
		.filter((p) => p !== "");
	return { entry: ports.length > 0 ? { protocol, ports } : { protocol } };
}

// --- findings ----------------------------------------------------------------

// Attached is a refused write's findings placed where they belong: on
// the draft element a path names (by its key), on a column the path
// names as a whole, or on the row when the path names neither.
export interface Attached {
	elements: Record<string, Finding[]>;
	columns: Partial<Record<"peers" | "services" | "description", Finding[]>>;
	row: Finding[];
}

export const noFindings: Attached = { elements: {}, columns: {}, row: [] };

const push = <K extends string>(
	into: Partial<Record<K, Finding[]>>,
	k: K,
	f: Finding,
) => {
	into[k] = [...(into[k] ?? []), f];
};

// attachFindings places findings whose paths are relative to the rule
// (after the prefix, e.g. `rulesets[2].rules[4]`). Findings outside the
// prefix are not this row's and are returned on the row.
export function attachFindings(
	d: RuleDraft,
	findings: readonly Finding[],
	prefix = "",
): Attached {
	const out: Attached = { elements: {}, columns: {}, row: [] };
	for (const f of findings) {
		let path = f.path;
		if (prefix) {
			if (path !== prefix && !path.startsWith(`${prefix}.`)) {
				out.row.push(f);
				continue;
			}
			path = path.slice(prefix.length).replace(/^\./, "");
		}
		const m = /^(peers|services|entries)\[(\d+)\]/.exec(path);
		if (m) {
			const list =
				m[1] === "peers" ? d.peers : m[1] === "services" ? d.refs : d.entries;
			const el = list[Number(m[2])];
			if (el) {
				push(out.elements, el.key, f);
				continue;
			}
		}
		if (path === "peers") push(out.columns, "peers", f);
		else if (path === "services" || path === "entries")
			push(out.columns, "services", f);
		else if (path === "description") push(out.columns, "description", f);
		else out.row.push(f);
	}
	return out;
}

// outstanding is how many attached findings still name something in
// the draft: removing the element a finding named takes it away.
export function outstanding(d: RuleDraft, a: Attached): number {
	const live = new Set([
		...d.peers.map((p) => p.key),
		...d.refs.map((r) => r.key),
		...d.entries.map((e) => e.key),
	]);
	let n = 0;
	for (const [k, fs] of Object.entries(a.elements)) {
		if (live.has(k)) n += fs.length;
	}
	for (const fs of Object.values(a.columns)) n += fs?.length ?? 0;
	return n + a.row.length;
}

// scopeFindings places findings on a scope's keys: `scope[key]` (or a
// preview's `selector[key]`) names one requirement; the bare path names
// the scope as a whole.
export function scopeFindings(
	findings: readonly Finding[],
	field: "scope" | "selector",
): { keys: Record<string, Finding[]>; whole: Finding[] } {
	const out = { keys: {} as Record<string, Finding[]>, whole: [] as Finding[] };
	for (const f of findings) {
		const m = new RegExp(`^${field}\\[(.*)\\]$`).exec(f.path);
		if (m?.[1] !== undefined) push(out.keys, m[1], f);
		else out.whole.push(f);
	}
	return out;
}

// --- the dry run -------------------------------------------------------------

// Edits are the editor's unsaved changes: to one persisted ruleset (its
// scope, and the row being edited), or a ruleset not yet created.
export interface Edits {
	rulesetId: string | null;
	ruleset?: Partial<Omit<RulesetInput, "rules">>;
	rule?: RuleDraft | null;
}

// DryRunPlan is a dry run's request and where the edited rule sits in
// it, so its findings come back to the row.
export interface DryRunPlan {
	body: DryRunRequest;
	rulePrefix: string | null;
	rulesetPrefix: string;
}

// planDryRun assembles the hypothetical set a dry run submits: every
// ruleset as the editor read it, with the edits applied, and the state
// version the editor read them at. The set is complete because the
// control plane renders it in place of the persisted one.
export function planDryRun(
	rulesets: readonly Ruleset[],
	stateVersion: string,
	edits: Edits,
): DryRunPlan {
	const set: RulesetInput[] = rulesets.map(rulesetInput);
	let at = set.findIndex((rs) => rs.id === edits.rulesetId);
	if (edits.rulesetId === null || at < 0) {
		const r = edits.ruleset ?? {};
		set.push({
			name: r.name ?? "",
			description: r.description,
			enabled: r.enabled ?? true,
			scope: r.scope ?? {},
			rules: [],
		});
		at = set.length - 1;
	} else {
		const cur = set[at] as RulesetInput;
		const r = edits.ruleset ?? {};
		set[at] = {
			...cur,
			name: r.name ?? cur.name,
			description: r.description ?? cur.description,
			enabled: r.enabled ?? cur.enabled,
			scope: r.scope ?? cur.scope,
		};
	}
	const target = set[at] as RulesetInput;
	let rulePrefix: string | null = null;
	if (edits.rule) {
		const input = draftInput(edits.rule, true);
		const rules = [...target.rules];
		const i = rules.findIndex((r) => r.id && r.id === edits.rule?.base?.id);
		const j = i >= 0 ? i : rules.length;
		rules[j] = input;
		target.rules = rules;
		rulePrefix = `rulesets[${at}].rules[${j}]`;
	}
	return {
		body: { state_version: stateVersion, rulesets: set },
		rulePrefix,
		rulesetPrefix: `rulesets[${at}]`,
	};
}

// The freshness of a dry run's result: computed on the state the editor
// read; computed after the state had moved (the control plane said so);
// or overtaken, the editor having read a newer state since.
export type Freshness = "current" | "stale" | "overtaken";

export function freshness(
	result: DryRunResult,
	currentStateVersion: string,
): Freshness {
	if (result.stale) return "stale";
	if (result.state_version !== currentStateVersion) return "overtaken";
	return "current";
}

// DeltaLine is one resolved rule a workload would gain, lose, or see
// change, named by the authored rule it came from.
export interface DeltaLine {
	kind: "added" | "removed" | "changed";
	rule: string;
	service: string;
	peers: string;
	// For a change, what moved: ports and the peers gained and lost.
	detail?: string;
}

const portText = (d: RenderedRuleDelta) =>
	d.ports.length === 0
		? d.protocol === "icmp"
			? d.protocol
			: `${d.protocol} (every port)`
		: `${d.protocol}/${d.ports
				.map((p) => (p.start === p.end ? `${p.start}` : `${p.start}-${p.end}`))
				.join(",")}`;

const peersText = (cidrs: readonly string[]) =>
	cidrs.length === 0
		? "no peers"
		: cidrs.length <= 3
			? cidrs.join(", ")
			: `${cidrs.slice(0, 3).join(", ")} +${cidrs.length - 3}`;

// ruleLabel names a resolved rule by the authored rule it came from: the
// edited row's temporary id is "this rule", any other by its
// description or short id.
export function ruleLabeler(
	rulesets: readonly Ruleset[],
	draft: RuleDraft | null,
): (resolvedId: string) => string {
	const byId = new Map<string, Rule>();
	for (const rs of rulesets)
		for (const r of rs.rules) if (r.id) byId.set(r.id, r);
	return (resolvedId) => {
		const authored = resolvedId.split("/")[0] ?? resolvedId;
		if (draft && (authored === draft.tempId || authored === draft.base?.id)) {
			return "this rule";
		}
		const r = byId.get(authored);
		return r?.description || shortRuleId(authored);
	};
}

export function deltaLines(
	w: DryRunResult["workloads"][number],
	label: (resolvedId: string) => string,
): DeltaLine[] {
	const out: DeltaLine[] = [];
	for (const a of w.added) {
		out.push({
			kind: "added",
			rule: label(a.id),
			service: portText(a),
			peers: peersText(a.peer_cidrs),
		});
	}
	for (const r of w.removed) {
		out.push({
			kind: "removed",
			rule: label(r.id),
			service: portText(r),
			peers: peersText(r.peer_cidrs),
		});
	}
	for (const c of w.changed) {
		const before = new Set(c.before.peer_cidrs);
		const after = new Set(c.after.peer_cidrs);
		const gained = c.after.peer_cidrs.filter((p) => !before.has(p));
		const lost = c.before.peer_cidrs.filter((p) => !after.has(p));
		const parts: string[] = [];
		if (portText(c.before) !== portText(c.after)) {
			parts.push(`${portText(c.before)} → ${portText(c.after)}`);
		}
		if (gained.length > 0) parts.push(`+ ${peersText(gained)}`);
		if (lost.length > 0) parts.push(`− ${peersText(lost)}`);
		out.push({
			kind: "changed",
			rule: label(c.after.id),
			service: portText(c.after),
			peers: peersText(c.after.peer_cidrs),
			detail: parts.join(" · "),
		});
	}
	return out;
}

// draftFromRule is a convenience for tests and the table: a persisted
// rule's draft with a fresh temporary id.
export function draftFromRule(rule: Rule | null): RuleDraft {
	return draftOf(rule, newTempId());
}

export function newTempId(): string {
	return crypto.randomUUID();
}

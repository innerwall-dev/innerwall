import type { Entry, Peer, Selector } from "@/api/schema";
import { parseRequirements } from "@/lib/labels";
import { parsePeer, parseService, selectorText } from "./model";

// editorPath is the policy editor at one ruleset, by the name the
// listing holds; with no name, the editor at its first ruleset. Every
// "open in policy editor" in the console links here or, from a pair of
// observed traffic, through pairLink.
export function editorPath(ruleset?: string | null): string {
	return ruleset
		? `/policy?${new URLSearchParams({ ruleset }).toString()}`
		: "/policy";
}

// Via names the screen a deep link came from, for the editor to say so.
export type Via = "map" | "review";

export const viaText: Record<Via, string> = {
	map: "the flow map",
	review: "the simulation review",
};

// PairContext is a pair of observed traffic as a screen hands it to the
// editor: the source as a rule would name it (empty when it cannot be
// named), the services it reached as `<protocol>/<port>` or `icmp`, and,
// when the stored windows attribute the traffic to one, the authored
// rule that admitted it.
export interface PairContext {
	via: Via;
	peers: Peer[];
	services: string[];
	rule?: string | null;
}

// PairTarget is where the link lands: a ruleset by name, or a new
// ruleset whose scope selects the pair's destination.
export type PairTarget = { ruleset: string } | { scope: Selector | null };

// peerText is a peer as the editor's peer input reads it back: a
// selector as its requirements, a CIDR as itself, an address group by
// its id.
export function peerText(p: Peer): string {
	if (p.workloads) return selectorText(p.workloads);
	if (p.address_group !== undefined) return p.address_group;
	return p.cidr ?? "";
}

// pairLink is the editor at the pair's target carrying the pair: the
// ruleset named, or a new ruleset's scope; the pair's peers and services;
// and the rule the traffic was attributed to. Both drawers build their
// links here, and the editor reads them back with pairSuggestion.
export function pairLink(target: PairTarget, ctx: PairContext): string {
	const q = new URLSearchParams();
	if ("ruleset" in target) {
		q.set("ruleset", target.ruleset);
	} else {
		q.set("new", "1");
		if (target.scope && Object.keys(target.scope).length > 0) {
			q.set("scope", selectorText(target.scope));
		}
	}
	q.set("via", ctx.via);
	for (const p of ctx.peers) q.append("peer", peerText(p));
	for (const s of ctx.services) q.append("service", s);
	if (ctx.rule) q.set("rule", ctx.rule);
	return `/policy?${q.toString()}`;
}

// Suggestion is a pair read back from the editor's address: the peers
// and services a new rule starts from, the rule to point at, and what
// could not be read (a peer outside the label grammar, say), which the
// editor shows rather than drops.
export interface Suggestion {
	via: Via;
	peers: Peer[];
	refs: string[];
	entries: Entry[];
	rule: string | null;
	unread: string[];
}

// pairSuggestion reads a pair from the editor's address through the
// editor's own peer and service parsers, so a link and a typed rule are
// read alike; an address carrying no pair is null.
export function pairSuggestion(q: URLSearchParams): Suggestion | null {
	const via = q.get("via");
	if (via !== "map" && via !== "review") return null;
	const s: Suggestion = {
		via,
		peers: [],
		refs: [],
		entries: [],
		rule: q.get("rule"),
		unread: [],
	};
	for (const text of q.getAll("peer")) {
		const p = parsePeer(text);
		if ("error" in p) s.unread.push(`${text}: ${p.error}`);
		else s.peers.push(p);
	}
	for (const text of q.getAll("service")) {
		const svc = parseService(text);
		if ("entry" in svc) s.entries.push(svc.entry);
		else s.refs.push(svc.ref);
	}
	return s;
}

// suggestedScope reads a new ruleset's scope from the address; text
// outside the label grammar is left out, and the scope input says why
// when the operator types it.
export function suggestedScope(q: URLSearchParams): Selector {
	const parsed = parseRequirements(q.get("scope") ?? "", {
		alternatives: true,
	});
	if (!parsed.ok) return {};
	const sel: Selector = {};
	for (const r of parsed.requirements) {
		sel[r.key] = [...(sel[r.key] ?? []), ...r.values];
	}
	return sel;
}

// carried is the pair a new ruleset's address carries, for the address
// of the ruleset once created: everything but the form's own fields.
export function carried(q: URLSearchParams, ruleset: string): URLSearchParams {
	const out = new URLSearchParams(q);
	out.delete("new");
	out.delete("scope");
	out.set("ruleset", ruleset);
	return out;
}

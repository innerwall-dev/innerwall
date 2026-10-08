import { describe, expect, it } from "vitest";
import type { DryRunResult, Rollup } from "@/api/schema";
import { minutesAgo, workload } from "@/test/fixtures";
import { rollupOf, wref } from "@/test/map";
import { rule, ruleset } from "@/test/review";
import {
	attachFindings,
	bannerText,
	deltaLines,
	dirty,
	draftInput,
	draftOf,
	freshness,
	matchedHosts,
	namesOf,
	outstanding,
	parsePeer,
	parseService,
	peerChip,
	planDryRun,
	recency,
	ruleLabeler,
	scopeFindings,
	scopeMix,
	servicesCell,
	trafficByRule,
} from "./model";

const ssh = {
	id: "5f000000-0000-4000-8000-000000000001",
	name: "ssh",
	entries: [{ protocol: "tcp" as const, ports: ["22"] }],
	version: "1",
	created_at: minutesAgo(600),
	updated_at: minutesAgo(600),
};
const vpn = {
	id: "6f000000-0000-4000-8000-000000000001",
	name: "corp-vpn",
	cidrs: ["10.40.0.0/16"],
	version: "1",
	created_at: minutesAgo(600),
	updated_at: minutesAgo(600),
};
const names = namesOf([ssh], [vpn]);

describe("cells", () => {
	it("draws a selector peer as one chip requiring all of its labels", () => {
		expect(
			peerChip(
				{ workloads: { env: ["prod"], app: ["storefront-api"] } },
				names,
			),
		).toEqual({
			tag: "LABELS",
			text: "app=storefront-api env=prod",
			reference: false,
			requirements: [
				{ key: "app", values: ["storefront-api"] },
				{ key: "env", values: ["prod"] },
			],
		});
		expect(peerChip({ workloads: { app: ["a", "b"] } }, names).text).toBe(
			"app=a|b",
		);
		expect(peerChip({ address_group: vpn.id }, names)).toEqual({
			tag: "GROUP",
			text: "corp-vpn",
			reference: true,
		});
		expect(peerChip({ cidr: "10.40.0.0/33" }, names)).toEqual({
			tag: "CIDR",
			text: "10.40.0.0/33",
			reference: false,
		});
	});

	it("lists the union of referenced services and inline entries, each in its own form", () => {
		expect(
			servicesCell(
				{
					services: [ssh.id, "svc_legacy_ldap"],
					entries: [
						{ protocol: "tcp", ports: ["389", "6000-6010"] },
						{ protocol: "icmp" },
						{ protocol: "udp" },
					],
				},
				names,
			),
		).toEqual([
			{ tag: "REF", text: "ssh", reference: true },
			{ tag: "REF", text: "svc_legacy_ldap", reference: true },
			{ tag: "TCP", text: "389, 6000-6010", reference: false },
			{ tag: "ICMP", text: "", reference: false },
			{ tag: "UDP", text: "every port", reference: false },
		]);
		expect(servicesCell({ services: [ssh.id] }, names)).toHaveLength(1);
		expect(servicesCell({ entries: [{ protocol: "tcp" }] }, names)).toEqual([
			{ tag: "TCP", text: "every port", reference: false },
		]);
	});

	it("cues a rule created or changed in the last day, from its instants", () => {
		const now = Date.parse("2026-10-04T12:00:00Z");
		const ago = (m: number) => new Date(now - m * 60_000).toISOString();
		expect(recency(rule({ created_at: ago(180) }), now)).toBe("added 3h ago");
		expect(
			recency(rule({ created_at: ago(4000), updated_at: ago(30) }), now),
		).toBe("changed 30m ago");
		expect(
			recency(rule({ created_at: ago(4000), updated_at: ago(3000) }), now),
		).toBeNull();
	});
});

describe("traffic", () => {
	const group = (
		authored: string,
		protocol: string,
		connections: number,
		workloads: number,
	) => ({
		keys: {
			rule: {
				id: `${authored}/${protocol}`,
				authored_rule_id: authored,
				protocol: protocol as "tcp",
			},
		},
		workload_count: workloads,
		flow_count: 1,
		connection_count: connections,
		byte_count: 0,
		first_seen: minutesAgo(60),
		last_seen: minutesAgo(1),
	});

	it("folds a rule's protocols, with the workload count a lower bound across them", () => {
		const r: Rollup = {
			...rollupOf([
				group("a", "tcp", 400_000, 40),
				group("b", "tcp", 88, 12),
				group("b", "udp", 12, 9),
				{ ...group("x", "tcp", 5, 1), keys: { rule: null } },
			]),
			group_by: ["rule"],
		};
		const t = trafficByRule(r);
		expect(t.get("a")).toEqual({
			connections: 400_000,
			workloads: 40,
			atLeast: false,
		});
		expect(t.get("b")).toEqual({
			connections: 100,
			workloads: 12,
			atLeast: true,
		});
		expect(t.size).toBe(2);
	});
});

describe("scope", () => {
	it("joins the preview with the scoped walk for the mode mix and the list", () => {
		const a = workload({ hostname: "checkout-prod-02", mode: "simulation" });
		const b = workload({
			hostname: "checkout-prod-07",
			mode: "simulation",
			sync: { state: "degraded", applied_version: 41, latest_version: 42 },
		});
		const c = workload({ hostname: "checkout-prod-01", mode: "visibility" });
		const d = workload({ hostname: "checkout-prod-03", mode: "enforced" });
		const m = {
			count: 4,
			matched: [a, b, c, d].map((w) => wref(w.id, w.hostname, w.labels)),
			byId: new Map([a, b, c, d].map((w) => [w.id, w])),
		};
		expect(scopeMix(m)).toEqual({
			simulation: 2,
			visibility: 1,
			enforced: 1,
			degraded: 1,
			offline: 0,
			pending: 0,
		});
		expect(matchedHosts(m).map((h) => [h.hostname, h.issue])).toEqual([
			["checkout-prod-07", "degraded"],
			["checkout-prod-01", null],
			["checkout-prod-02", null],
			["checkout-prod-03", null],
		]);
	});

	it("states the immediate effect, and what enforced workloads do only when there are some", () => {
		const mix = {
			simulation: 40,
			visibility: 2,
			enforced: 0,
			degraded: 1,
			offline: 0,
			pending: 0,
		};
		expect(bannerText({ enabled: true, count: 42, mix })).toBe(
			"There is no draft. Saving an enabled rule re-renders the 42 workloads in scope and pushes the change to their agents immediately. Workloads in simulation still drop nothing.",
		);
		expect(
			bannerText({ enabled: true, count: 3, mix: { ...mix, enforced: 1 } }),
		).toContain(
			"1 enforced workload drops what no enabled rule admits; workloads in simulation still drop nothing.",
		);
		expect(bannerText({ enabled: false, count: 42, mix })).toContain(
			"This ruleset is disabled, so its rules render onto no workload",
		);
	});

	it("places a scope's findings on its keys", () => {
		expect(
			scopeFindings(
				[
					{ path: "scope[tier]", rule: "label-values-required", message: "m" },
					{ path: "scope", rule: "selector-empty", message: "e" },
				],
				"scope",
			),
		).toEqual({
			keys: {
				tier: [
					{ path: "scope[tier]", rule: "label-values-required", message: "m" },
				],
			},
			whole: [{ path: "scope", rule: "selector-empty", message: "e" }],
		});
	});
});

describe("drafts and findings", () => {
	it("reads typed peers and services: selectors through the label grammar, the rest kept for admission", () => {
		expect(parsePeer("app=storefront-api env=prod")).toEqual({
			workloads: { app: ["storefront-api"], env: ["prod"] },
		});
		expect(parsePeer("app = a | b")).toEqual({
			workloads: { app: ["a", "b"] },
		});
		expect(parsePeer("tier=")).toEqual({ error: "tier has no value." });
		expect(parsePeer('app="web env"')).toHaveProperty("error");
		expect(parsePeer("10.40.0.0/33")).toEqual({ cidr: "10.40.0.0/33" });
		expect(parsePeer("corp-vpn")).toEqual({ address_group: "corp-vpn" });
		expect(parseService("TCP/389")).toEqual({
			entry: { protocol: "tcp", ports: ["389"] },
		});
		expect(parseService("tcp/6000-6010, 6012")).toEqual({
			entry: { protocol: "tcp", ports: ["6000-6010", "6012"] },
		});
		expect(parseService("icmp")).toEqual({ entry: { protocol: "icmp" } });
		expect(parseService("svc_legacy_ldap")).toEqual({ ref: "svc_legacy_ldap" });
	});

	it("writes a new rule without an id, and a persisted one under its own", () => {
		const fresh = draftOf(null, "temp-1");
		expect(draftInput(fresh)).not.toHaveProperty("id");
		expect(draftInput(fresh, true).id).toBe("temp-1");
		const r = rule();
		const d = draftOf(r, "temp-2");
		expect(draftInput(d, true).id).toBe(r.id);
		expect(dirty(d)).toBe(false);
		expect(dirty({ ...d, description: "changed" })).toBe(true);
		expect(dirty(fresh)).toBe(true);
	});

	it("attaches findings at the paths they name, and drops one with its element", () => {
		const d = draftOf(null, "t");
		d.peers = [
			{ key: "p0", value: { cidr: "10.40.0.0/33" } },
			{ key: "p1", value: { workloads: { tier: [] } } },
		];
		d.refs = [{ key: "s0", value: "svc_legacy_ldap" }];
		d.entries = [{ key: "e0", value: { protocol: "tcp", ports: ["389"] } }];
		const cidr = {
			path: "rulesets[0].rules[4].peers[0].cidr",
			rule: "cidr",
			message: "10.40.0.0/33 is not a valid CIDR",
		};
		const tier = {
			path: "rulesets[0].rules[4].peers[1].workloads[tier]",
			rule: "label-values-required",
			message: "no values",
		};
		const ref = {
			path: "rulesets[0].rules[4].services[0]",
			rule: "service-unknown",
			message: "unknown",
		};
		const other = {
			path: "rulesets[1].name",
			rule: "name-required",
			message: "",
		};
		const a = attachFindings(
			d,
			[cidr, tier, ref, other],
			"rulesets[0].rules[4]",
		);
		expect(a.elements).toEqual({ p0: [cidr], p1: [tier], s0: [ref] });
		expect(a.row).toEqual([other]);
		expect(outstanding(d, a)).toBe(4);
		// Removing the bad prefix takes its finding with it; the others stay
		// on their own elements.
		const fixed = { ...d, peers: d.peers.slice(1) };
		expect(outstanding(fixed, a)).toBe(3);
		// A rule-level write's paths are relative to the rule.
		const b = attachFindings(d, [
			{
				path: "peers[1].workloads[tier]",
				rule: "label-values-required",
				message: "",
			},
			{ path: "services", rule: "services-required", message: "" },
		]);
		expect(Object.keys(b.elements)).toEqual(["p1"]);
		expect(b.columns.services).toHaveLength(1);
	});
});

describe("the dry run", () => {
	const api = rule({ description: "api-to-checkout" });
	const scrape = rule({ description: "metrics-scrape", enabled: false });
	const checkout = ruleset("checkout-inbound", { app: ["checkout"] }, [
		api,
		scrape,
	]);
	const auth = ruleset("auth-inbound", { app: ["auth"] }, [rule()]);

	it("submits every ruleset as read, with the row's edit in place, at the state version read", () => {
		const d = draftOf(scrape, "t1");
		d.enabled = true;
		const plan = planDryRun([auth, checkout], "sv-1", {
			rulesetId: checkout.id ?? null,
			rule: d,
		});
		expect(plan.body.state_version).toBe("sv-1");
		expect(plan.body.rulesets.map((r) => r.name)).toEqual([
			"auth-inbound",
			"checkout-inbound",
		]);
		const edited = plan.body.rulesets[1];
		expect(edited?.rules.map((r) => [r.id, r.enabled])).toEqual([
			[api.id, true],
			[scrape.id, true],
		]);
		// Versions and instants are not part of what is authored.
		expect(edited?.rules[0]).not.toHaveProperty("version");
		expect(edited).not.toHaveProperty("created_at");
		expect(plan.rulePrefix).toBe("rulesets[1].rules[1]");
		// The untouched ruleset goes as it was read.
		expect(plan.body.rulesets[0]?.rules[0]?.id).toBe(auth.rules[0]?.id);
	});

	it("appends a new rule under its temporary id, and applies a scope edit", () => {
		const d = draftOf(null, "temp-new");
		d.peers = [{ key: "p", value: { cidr: "10.40.0.0/16" } }];
		const plan = planDryRun([checkout], "sv-1", {
			rulesetId: checkout.id ?? null,
			ruleset: { scope: { app: ["checkout"], env: ["prod"] } },
			rule: d,
		});
		const rs = plan.body.rulesets[0];
		expect(rs?.scope).toEqual({ app: ["checkout"], env: ["prod"] });
		expect(rs?.rules.map((r) => r.id)).toEqual([api.id, scrape.id, "temp-new"]);
		expect(plan.rulePrefix).toBe("rulesets[0].rules[2]");
	});

	it("adds a ruleset not yet created at the end of the set", () => {
		const plan = planDryRun([checkout], "sv-1", {
			rulesetId: null,
			ruleset: { name: "ledger-inbound", scope: { app: ["ledger"] } },
		});
		expect(plan.body.rulesets[1]).toEqual({
			name: "ledger-inbound",
			description: undefined,
			enabled: true,
			scope: { app: ["ledger"] },
			rules: [],
		});
		expect(plan.rulesetPrefix).toBe("rulesets[1]");
		expect(plan.rulePrefix).toBeNull();
	});

	it("tells a stale result from one the editor has since overtaken", () => {
		const r = (state_version: string, stale: boolean): DryRunResult => ({
			state_version,
			stale,
			workloads: [],
		});
		expect(freshness(r("sv-1", false), "sv-1")).toBe("current");
		expect(freshness(r("sv-2", true), "sv-1")).toBe("stale");
		expect(freshness(r("sv-1", false), "sv-2")).toBe("overtaken");
	});

	it("names each delta by the rule it came from", () => {
		const d = draftOf(null, "temp-new");
		const label = ruleLabeler([checkout], d);
		const lines = deltaLines(
			{
				workload: wref("w1", "checkout-prod-01", {}),
				version: 42,
				mode: null,
				added: [
					{
						id: "temp-new/tcp",
						protocol: "tcp",
						ports: [{ start: 389, end: 389 }],
						peer_cidrs: ["10.40.0.0/16"],
					},
				],
				removed: [
					{
						id: `${api.id}/tcp`,
						protocol: "tcp",
						ports: [{ start: 8443, end: 8443 }],
						peer_cidrs: ["10.0.0.1/32"],
					},
				],
				changed: [
					{
						before: {
							id: `${scrape.id}/tcp`,
							protocol: "tcp",
							ports: [{ start: 9100, end: 9100 }],
							peer_cidrs: ["10.0.0.5/32", "10.0.0.6/32"],
						},
						after: {
							id: `${scrape.id}/tcp`,
							protocol: "tcp",
							ports: [{ start: 9100, end: 9101 }],
							peer_cidrs: ["10.0.0.6/32", "10.0.0.7/32"],
						},
					},
				],
			},
			label,
		);
		expect(lines).toEqual([
			{
				kind: "added",
				rule: "this rule",
				service: "tcp/389",
				peers: "10.40.0.0/16",
			},
			{
				kind: "removed",
				rule: "api-to-checkout",
				service: "tcp/8443",
				peers: "10.0.0.1/32",
			},
			{
				kind: "changed",
				rule: "metrics-scrape",
				service: "tcp/9100-9101",
				peers: "10.0.0.6/32, 10.0.0.7/32",
				detail: "tcp/9100 → tcp/9100-9101 · + 10.0.0.7/32 · − 10.0.0.5/32",
			},
		]);
	});
});

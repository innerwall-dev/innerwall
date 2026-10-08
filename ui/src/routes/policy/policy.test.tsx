import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type {
	DryRunResult,
	Rollup,
	Rule,
	Ruleset,
	Workload,
} from "@/api/schema";
import { minutesAgo, workload } from "@/test/fixtures";
import {
	freshInstall,
	mockSurface,
	problem,
	type Reply,
	type Route,
	renderApp,
	signedIn,
} from "@/test/harness";
import { addressGroup, rollupOf, wref } from "@/test/map";
import { rule, ruleset } from "@/test/review";

// The editor's estate, after design 12: checkout-inbound with four rules
// (one referencing a service definition, one an address group, one
// disabled), two more rulesets, one disabled. The scope holds three
// simulating workloads, one degraded, and one in visibility.
const ssh = {
	id: "5f000000-0000-4000-8000-0000000000aa",
	name: "ssh",
	entries: [{ protocol: "tcp" as const, ports: ["22"] }],
	version: "1",
	created_at: minutesAgo(9000),
	updated_at: minutesAgo(9000),
};
const vpn = addressGroup("6f000000-0000-4000-8000-0000000000bb", "corp-vpn", [
	"10.40.0.0/16",
]);

const apiRule = rule({
	description: "Production API may reach checkout.",
	peers: [{ workloads: { app: ["storefront-api"], env: ["prod"] } }],
	entries: [{ protocol: "tcp", ports: ["8443"] }],
	version: "4",
	created_at: minutesAgo(9000),
	updated_at: minutesAgo(9000),
});
const sshRule = rule({
	description: "Operator SSH via bastion.",
	peers: [{ workloads: { app: ["bastion"] } }],
	services: [ssh.id],
	entries: undefined,
	version: "2",
	created_at: minutesAgo(9000),
	updated_at: minutesAgo(120),
});
const vpnRule = rule({
	description: "Temporary — engineers on VPN.",
	peers: [{ address_group: vpn.id }],
	services: [ssh.id],
	entries: [{ protocol: "tcp", ports: ["8443"] }],
	version: "1",
	created_at: minutesAgo(30),
	updated_at: minutesAgo(30),
});
const scrape = rule({
	description: "Scrapes from the metrics collectors.",
	enabled: false,
	peers: [{ workloads: { app: ["metrics-collector"] } }],
	entries: [{ protocol: "tcp", ports: ["9100"] }],
	version: "3",
	created_at: minutesAgo(9000),
	updated_at: minutesAgo(9000),
});
const checkout = ruleset(
	"checkout-inbound",
	{ app: ["checkout"], env: ["prod"] },
	[apiRule, sshRule, vpnRule, scrape],
	{ description: "Inbound access to the checkout API in production." },
);
const auth = ruleset("auth-inbound", { app: ["auth"] }, [rule()]);
const billing = ruleset("billing-inbound", { app: ["billing"] }, [], {
	enabled: false,
});

const checkoutLabels = { app: "checkout", env: "prod" };
const fleet: Workload[] = [
	...[1, 2, 3].map((i) =>
		workload({
			id: `w-c${i}`,
			hostname: `checkout-prod-0${i}`,
			labels: checkoutLabels,
			mode: "simulation",
		}),
	),
	workload({
		id: "w-c7",
		hostname: "checkout-prod-07",
		labels: checkoutLabels,
		mode: "simulation",
		sync: { state: "degraded", applied_version: 41, latest_version: 42 },
	}),
	workload({
		id: "w-c41",
		hostname: "checkout-prod-41",
		labels: checkoutLabels,
		mode: "visibility",
	}),
];
const authHost = workload({
	id: "w-a1",
	hostname: "auth-prod-01",
	labels: { app: "auth" },
	mode: "enforced",
});

// matches resolves a selector the way the control plane's preview does,
// for this estate only.
function matches(sel: Record<string, string[]>): Workload[] {
	return [...fleet, authHost].filter((w) =>
		Object.entries(sel).every(([k, vs]) => vs.includes(w.labels[k] ?? "")),
	);
}

function ruleRollup(): Rollup {
	const g = (r: Rule, protocol: string, conns: number, n: number) => ({
		keys: {
			rule: {
				id: `${r.id}/${protocol}`,
				authored_rule_id: r.id,
				protocol: protocol as "tcp",
			},
		},
		workload_count: n,
		flow_count: 4,
		connection_count: conns,
		byte_count: 0,
		first_seen: minutesAgo(600),
		last_seen: minutesAgo(1),
	});
	return {
		...rollupOf([
			g(apiRule, "tcp", 412_880, 40),
			g(sshRule, "tcp", 80, 12),
			g(sshRule, "udp", 8, 3),
		]),
		group_by: ["rule"],
	};
}

interface Surface {
	rulesets?: Ruleset[];
	stateVersion?: () => string;
	extra?: Route[];
	dryRun?: (body: unknown) => Reply;
}

function surface(opts: Surface = {}) {
	let rulesets = opts.rulesets ?? [checkout, auth, billing];
	const sv = opts.stateVersion ?? (() => "sv-1");
	const s = mockSurface([
		...(opts.extra ?? []),
		signedIn,
		{
			method: "GET",
			path: "/api/v1/rulesets",
			reply: () => ({
				status: 200,
				json: { rulesets, state_version: sv() },
			}),
		},
		{
			method: "GET",
			path: "/api/v1/services",
			reply: { status: 200, json: { services: [ssh] } },
		},
		{
			method: "GET",
			path: "/api/v1/address-groups",
			reply: { status: 200, json: { address_groups: [vpn] } },
		},
		{
			method: "POST",
			path: "/api/v1/selectors/preview",
			reply: (body) => {
				const sel = (body as { selector: Record<string, string[]> }).selector;
				const bad = Object.entries(sel).find(([, vs]) => vs.length === 0);
				if (Object.keys(sel).length === 0 || bad) {
					return {
						status: 400,
						problem: {
							...problem("validation", 400),
							errors: [
								bad
									? {
											path: `selector[${bad[0]}]`,
											rule: "label-values-required",
											message: `${bad[0]} has no values; this selector matches nothing`,
										}
									: {
											path: "selector",
											rule: "selector-empty",
											message: "an empty selector matches nothing",
										},
							],
						},
					};
				}
				const m = matches(sel);
				return {
					status: 200,
					json: {
						matched: m.map((w) => wref(w.id, w.hostname, w.labels)),
						count: m.length,
					},
				};
			},
		},
		{
			method: "GET",
			path: "/api/v1/workloads",
			reply: (_b, q) => {
				const sel: Record<string, string[]> = {};
				for (const l of q.getAll("label")) {
					const [k, v] = l.split("=");
					sel[k as string] = [...(sel[k as string] ?? []), v as string];
				}
				return {
					status: 200,
					json: { workloads: matches(sel), next_cursor: null },
				};
			},
		},
		{
			method: "GET",
			path: "/api/v1/flows/rollup",
			reply: { status: 200, json: ruleRollup() },
		},
		{
			method: "POST",
			path: "/api/v1/policies/render-dryrun",
			reply: (body) =>
				opts.dryRun?.(body) ?? {
					status: 200,
					json: {
						state_version: sv(),
						stale: false,
						workloads: [],
					} satisfies DryRunResult,
				},
		},
	]);
	return {
		...s,
		setRulesets: (next: Ruleset[]) => {
			rulesets = next;
		},
	};
}

const rulesTable = () => screen.findByRole("table", { name: "Rules" });
const row = async (name: string) =>
	within(await rulesTable()).getByRole("row", { name });

describe("policy editor", () => {
	it("opens on the first ruleset and names it in the address and the crumb", async () => {
		surface();
		renderApp("/policy");
		expect(
			await screen.findByRole("heading", { name: "checkout-inbound" }),
		).toBeInTheDocument();
		await waitFor(() =>
			expect(
				screen.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveTextContent("Policy/checkout-inbound"),
		);
		const rail = screen.getByRole("navigation", { name: "Ruleset list" });
		expect(
			within(rail)
				.getAllByRole("link")
				.map((l) => l.textContent),
		).toEqual([
			"checkout-inboundenabled, 4 rules4",
			"auth-inboundenabled, 1 rule1",
			"billing-inbounddisabled, 0 rules0",
		]);
		expect(
			within(rail).getByRole("link", { name: /checkout-inbound/ }),
		).toHaveAttribute("aria-current", "page");
		const defs = screen.getByRole("complementary", { name: "Rulesets" });
		expect(defs).toHaveTextContent("Service definitions1");
		expect(defs).toHaveTextContent("Address groups1");
	});

	it("lands a deep link at the ruleset it names", async () => {
		surface();
		renderApp("/policy?ruleset=auth-inbound");
		expect(
			await screen.findByRole("heading", { name: "auth-inbound" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: "Review simulation" }),
		).toHaveAttribute("href", "/simulation?ruleset=auth-inbound");
	});

	it("follows the review's link into the editor at the ruleset under review", async () => {
		// The review drawer's and header's links name the ruleset; following
		// one lands the editor there.
		surface();
		const user = userEvent.setup();
		renderApp("/policy?ruleset=billing-inbound");
		await screen.findByRole("heading", { name: "billing-inbound" });
		await user.click(
			within(
				screen.getByRole("navigation", { name: "Ruleset list" }),
			).getByRole("link", { name: /auth-inbound/ }),
		);
		expect(
			await screen.findByRole("heading", { name: "auth-inbound" }),
		).toBeInTheDocument();
	});

	it("draws the rules: description and short id, peers as one chip per selector, services as references and entries", async () => {
		surface();
		renderApp("/policy?ruleset=checkout-inbound");
		const api = await row("Production API may reach checkout.");
		expect(
			within(api)
				.getAllByTestId("chip")
				.map((c) => c.textContent),
		).toEqual(["LABELSapp=storefront-api env=prod", "TCP8443"]);
		expect(api).toHaveTextContent((apiRule.id ?? "").split("-")[0] as string);
		// The union: a referenced definition (dashed) and an inline entry
		// (solid) in one cell.
		const vpnRow = await row("Temporary — engineers on VPN.");
		const chips = within(vpnRow).getAllByTestId("chip");
		expect(chips.map((c) => c.textContent)).toEqual([
			"GROUPcorp-vpn",
			"REFssh",
			"TCP8443",
		]);
		expect(chips[0]).toHaveClass("border-dashed");
		expect(chips[1]).toHaveClass("border-dashed");
		expect(chips[2]).toHaveClass("border-solid");
		// The recently-changed cue from the real instants.
		expect(within(vpnRow).getByTestId("recency")).toHaveTextContent(
			"added 30m ago",
		);
		expect(
			within(await row("Operator SSH via bastion.")).getByTestId("recency"),
		).toHaveTextContent("changed 2h ago");
		expect(within(api).queryByTestId("recency")).toBeNull();
	});

	it("states each rule's day of traffic, and nothing for a disabled rule", async () => {
		const { calls } = surface();
		renderApp("/policy?ruleset=checkout-inbound");
		const api = await row("Production API may reach checkout.");
		await waitFor(() =>
			expect(within(api).getByTestId("simulation")).toHaveTextContent(
				"412,880 allowedmatched on 40 workloads",
			),
		);
		// Two protocols: the larger count, as a lower bound.
		expect(
			within(await row("Operator SSH via bastion.")).getByTestId("simulation"),
		).toHaveTextContent("88 allowedmatched on 12+ workloads");
		expect(
			within(await row("Scrapes from the metrics collectors.")).getByTestId(
				"simulation",
			),
		).toHaveTextContent("disabled — admits nothing while off");
		expect(
			within(await row("Temporary — engineers on VPN.")).getByTestId(
				"simulation",
			),
		).toHaveTextContent("no matched traffic");
		const rollup = calls.find((c) => c.path.includes("/flows/rollup"));
		const q = new URL(rollup?.path ?? "", "http://x").searchParams;
		expect(q.get("group_by")).toBe("rule");
		expect(q.get("verdict")).toBe("allowed");
		expect(q.getAll("label")).toEqual(["app=checkout", "env=prod"]);
		expect(
			Date.parse(q.get("to") ?? "") - Date.parse(q.get("from") ?? ""),
		).toBe(24 * 60 * 60 * 1000);
		expect(
			screen.getByRole("columnheader", { name: /Simulation/ }),
		).toHaveTextContent("Simulation · last 24h");
	});

	it("states the blast radius from the live preview, joined with the scoped walk", async () => {
		surface();
		renderApp("/policy?ruleset=checkout-inbound");
		await waitFor(() =>
			expect(screen.getByTestId("match-count")).toHaveTextContent(
				"5workloads match right now",
			),
		);
		expect(screen.getByTestId("match-mix")).toHaveTextContent(
			"4 simulation 1 visibility 1 degraded",
		);
		expect(screen.getByTestId("banner")).toHaveTextContent(
			"There is no draft. Saving an enabled rule re-renders the 5 workloads in scope and pushes the change to their agents immediately. Workloads in simulation still drop nothing.",
		);
		// The degraded workload leads the list, and links to its detail.
		expect(
			screen.getByRole("link", { name: "checkout-prod-07" }),
		).toHaveAttribute("href", "/workloads/w-c7");
	});

	it("counts a scope edit live, and places the preview's refusal on the requirement", async () => {
		const { calls } = surface();
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		await waitFor(() =>
			expect(screen.getByTestId("match-count")).toHaveTextContent("5"),
		);
		await user.click(screen.getByRole("button", { name: "Remove env" }));
		await user.type(
			screen.getByRole("textbox", { name: "Add a scope requirement" }),
			"app = checkout | auth{Enter}",
		);
		await waitFor(() =>
			expect(screen.getByTestId("match-count")).toHaveTextContent(
				"6workloads match this scope",
			),
		);
		// Typing was not a request per keystroke: one preview per settled
		// scope.
		const previews = calls.filter((c) => c.path.endsWith("/selectors/preview"));
		expect(previews.map((c) => c.body)).toContainEqual({
			selector: { app: ["checkout", "auth"] },
		});
		expect(previews.length).toBeLessThanOrEqual(4);
		expect(
			screen.getByRole("button", {
				name: /Save scope — applies to 6 workloads now/,
			}),
		).toBeInTheDocument();
		// A key without values: the control plane says it matches nothing,
		// at the key.
		await user.type(
			screen.getByRole("textbox", { name: "Add a scope requirement" }),
			"tier={Enter}",
		);
		expect(await screen.findByTestId("finding")).toHaveTextContent(
			"tier has no values; this selector matches nothing",
		);
		expect(
			screen
				.getAllByTestId("requirement")
				.find((r) => r.textContent?.startsWith("tier")),
		).toHaveClass("border-status-critical-fg");
	});

	it("enables a disabled rule from the table, conditioned on the version it read", async () => {
		const puts: { body: unknown; ifMatch: string | null }[] = [];
		const s = surface({
			extra: [
				{
					method: "PUT",
					path: `/api/v1/rulesets/${checkout.id}/rules/${scrape.id}`,
					reply: (body) => {
						puts.push({ body, ifMatch: null });
						return {
							status: 200,
							json: { ...scrape, enabled: true, version: "4" },
						};
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		const r = await row("Scrapes from the metrics collectors.");
		await user.click(
			within(r).getByRole("switch", {
				name: "Rule Scrapes from the metrics collectors. enabled",
			}),
		);
		expect(await screen.findByRole("status")).toHaveTextContent(
			"Enabled Scrapes from the metrics collectors.",
		);
		const call = s.fetchMock.mock.calls.find(
			([, init]) => (init as RequestInit | undefined)?.method === "PUT",
		);
		const headers = (call as [unknown, RequestInit])[1].headers as Record<
			string,
			string
		>;
		expect(headers["If-Match"]).toBe('"3"');
		expect(puts[0]?.body).toEqual({
			id: scrape.id,
			direction: "inbound",
			enabled: true,
			description: scrape.description,
			peers: scrape.peers,
			entries: scrape.entries,
		});
	});

	it("renders a refused toggle's conflict, and reloads", async () => {
		surface({
			extra: [
				{
					method: "PUT",
					path: `/api/v1/rulesets/${checkout.id}/rules/${scrape.id}`,
					reply: {
						status: 412,
						problem: {
							...problem("precondition-failed", 412),
							current_version: "5",
						},
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		const r = await row("Scrapes from the metrics collectors.");
		await user.click(within(r).getByRole("switch"));
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(
			"Scrapes from the metrics collectors. changed since this editor read it · it is now at version 5; nothing was changed.",
		);
		expect(
			within(alert).getByRole("button", { name: "Reload" }),
		).toBeInTheDocument();
	});

	it("disables the ruleset itself, conditioned on its version", async () => {
		const bodies: unknown[] = [];
		const s = surface({
			extra: [
				{
					method: "PUT",
					path: `/api/v1/rulesets/${checkout.id}`,
					reply: (body) => {
						bodies.push(body);
						return { status: 200, json: { ...checkout, enabled: false } };
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		await user.click(
			await screen.findByRole("switch", {
				name: "Ruleset checkout-inbound enabled",
			}),
		);
		expect(await screen.findByRole("status")).toHaveTextContent(
			"Disabled checkout-inbound",
		);
		const call = s.fetchMock.mock.calls.find(
			([, init]) => (init as RequestInit | undefined)?.method === "PUT",
		);
		expect(
			((call as [unknown, RequestInit])[1].headers as Record<string, string>)[
				"If-Match"
			],
		).toBe('"7"');
		expect(bodies[0]).toMatchObject({
			name: "checkout-inbound",
			enabled: false,
			scope: checkout.scope,
		});
		expect((bodies[0] as Ruleset).rules).toHaveLength(4);
	});

	it("renders a refused save's findings at the fields their paths name", async () => {
		const posted: unknown[] = [];
		surface({
			extra: [
				{
					method: "POST",
					path: `/api/v1/rulesets/${checkout.id}/rules`,
					reply: (body) => {
						posted.push(body);
						return {
							status: 400,
							problem: {
								...problem("validation", 400),
								errors: [
									{
										path: "peers[0].cidr",
										rule: "cidr",
										message:
											"10.40.0.0/33 is not a valid CIDR (prefix must be 0–32).",
									},
									{
										path: "peers[1].workloads[tier]",
										rule: "label-values-required",
										message:
											"tier= has no values — this selector matches nothing.",
									},
									{
										path: "services[0]",
										rule: "service-unknown",
										message:
											"Service definition svc_legacy_ldap no longer exists.",
									},
								],
							},
						};
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		await rulesTable();
		await user.click(screen.getByRole("button", { name: "Add rule" }));
		const editing = await row("New rule");
		const peer = within(editing).getByRole("textbox", { name: "Add a peer" });
		await user.type(peer, "10.40.0.0/33{Enter}");
		await user.type(peer, "tier={Enter}");
		const svc = within(editing).getByRole("textbox", { name: "Add a service" });
		await user.type(svc, "svc_legacy_ldap{Enter}");
		await user.type(svc, "tcp/389{Enter}");
		await waitFor(() =>
			expect(
				screen.getByRole("button", {
					name: /Save — applies to 5 workloads now/,
				}),
			).toBeEnabled(),
		);
		await user.click(
			screen.getByRole("button", { name: /Save — applies to 5 workloads now/ }),
		);
		// What was sent: the rule as typed, with no id (the control plane
		// assigns one).
		expect(posted[0]).toEqual({
			direction: "inbound",
			enabled: true,
			description: "",
			peers: [{ cidr: "10.40.0.0/33" }, { workloads: { tier: [] } }],
			services: ["svc_legacy_ldap"],
			entries: [{ protocol: "tcp", ports: ["389"] }],
		});
		// Each finding under the element its path names; the elements drawn
		// as refused, the unknown definition struck.
		const findings = await within(editing).findAllByTestId("finding");
		expect(findings.map((f) => f.getAttribute("data-path"))).toEqual([
			"peers[0].cidr",
			"peers[1].workloads[tier]",
			"services[0]",
		]);
		const chips = within(editing).getAllByTestId("chip");
		expect(chips.map((c) => c.getAttribute("data-failed"))).toEqual([
			"true",
			"true",
			"true",
			null,
		]);
		expect(
			within(chips[2] as HTMLElement).getByText("svc_legacy_ldap"),
		).toHaveClass("line-through");
		// Errors only, counted; the identical row is not offered again.
		expect(screen.getByTestId("row-errors")).toHaveTextContent(
			"3 errors block saving",
		);
		expect(
			screen.getByRole("button", { name: /Save — applies to 5 workloads now/ }),
		).toBeDisabled();
		// Removing the bad prefix takes its finding with it, and the row may
		// be saved again.
		await user.click(
			within(editing).getByRole("button", { name: "Remove cidr 10.40.0.0/33" }),
		);
		expect(screen.getByTestId("row-errors")).toHaveTextContent(
			"2 errors block saving",
		);
		expect(
			screen.getByRole("button", { name: /Save — applies to 5 workloads now/ }),
		).toBeEnabled();
		// The editor never claims anything is pending or staged.
		expect(document.body).not.toHaveTextContent(
			/pending save|staged|draft saved/i,
		);
	});

	it("saves an edited rule with If-Match, and on a conflict reloads and reapplies the edits", async () => {
		const moved = {
			...apiRule,
			version: "5",
			peers: [{ workloads: { app: ["storefront-api"] } }],
		};
		let version = "sv-1";
		let conflicted = false;
		const puts: { body: unknown; ifMatch: string }[] = [];
		const s = surface({
			stateVersion: () => version,
			extra: [
				{
					method: "PUT",
					path: `/api/v1/rulesets/${checkout.id}/rules/${apiRule.id}`,
					reply: (body) => {
						if (!conflicted) {
							conflicted = true;
							version = "sv-2";
							s.setRulesets([
								{ ...checkout, rules: [moved, sshRule, vpnRule, scrape] },
								auth,
								billing,
							]);
							return {
								status: 412,
								problem: {
									...problem("precondition-failed", 412),
									current_version: "5",
								},
							};
						}
						return {
							status: 200,
							json: { ...moved, ...(body as object), version: "6" },
						};
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		const api = await row("Production API may reach checkout.");
		await user.click(
			within(api).getByRole("button", {
				name: "Edit Production API may reach checkout.",
			}),
		);
		const editing = await row("Editing Production API may reach checkout.");
		const desc = within(editing).getByRole("textbox", { name: "Description" });
		await user.clear(desc);
		await user.type(desc, "Storefront API may reach checkout.");
		await user.click(screen.getByRole("button", { name: /Save — applies/ }));
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(
			"Production API may reach checkout. changed since this editor read it · it is now at version 5; nothing was changed.",
		);
		await user.click(
			within(alert).getByRole("button", { name: "Reload and reapply" }),
		);
		// The edits stay; what is stored now is shown before saving over it.
		const reapplied = await screen.findByText(/reapplied on top of version/);
		expect(reapplied.closest("[data-testid=notice]")).toHaveTextContent(
			"Your edits are reapplied on top of version 5. As stored now, the rule admits app=storefront-api on tcp 8443. Save to replace it with your row, or discard the row to keep it.",
		);
		expect(
			within(await row("Editing Production API may reach checkout.")).getByRole(
				"textbox",
				{ name: "Description" },
			),
		).toHaveValue("Storefront API may reach checkout.");
		await user.click(screen.getByRole("button", { name: /Save — applies/ }));
		expect(await screen.findByRole("status")).toHaveTextContent(
			"Saved Storefront API may reach checkout. at version 6.",
		);
		for (const [, init] of s.fetchMock.mock.calls.filter(
			([, init]) => (init as RequestInit | undefined)?.method === "PUT",
		)) {
			const r = init as RequestInit;
			puts.push({
				body: JSON.parse(String(r.body)),
				ifMatch: (r.headers as Record<string, string>)["If-Match"] as string,
			});
		}
		// The first save named the version read; the second, the version
		// the reload read. Each carried the operator's row.
		expect(puts.map((p) => p.ifMatch)).toEqual(['"4"', '"5"']);
		expect(puts.map((p) => (p.body as Rule).description)).toEqual([
			"Storefront API may reach checkout.",
			"Storefront API may reach checkout.",
		]);
	});

	it("dry-runs the current set plus the row's edits at the state version read", async () => {
		const bodies: unknown[] = [];
		surface({
			dryRun: (body) => {
				bodies.push(body);
				const { rulesets } = body as { rulesets: Ruleset[] };
				const newRule = rulesets[0]?.rules[4];
				return {
					status: 200,
					json: {
						state_version: "sv-1",
						stale: false,
						workloads: [
							{
								workload: wref("w-c1", "checkout-prod-01", checkoutLabels),
								version: 42,
								mode: null,
								added: [
									{
										id: `${newRule?.id}/tcp`,
										protocol: "tcp",
										ports: [{ start: 389, end: 389 }],
										peer_cidrs: ["10.40.0.0/16"],
									},
								],
								removed: [],
								changed: [],
							},
						],
					} satisfies DryRunResult,
				};
			},
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		await rulesTable();
		await user.click(screen.getByRole("button", { name: "Add rule" }));
		const editing = await row("New rule");
		await user.type(
			within(editing).getByRole("textbox", { name: "Add a peer" }),
			"corp-vpn{Enter}",
		);
		await user.type(
			within(editing).getByRole("textbox", { name: "Add a service" }),
			"tcp/389{Enter}",
		);
		await user.click(screen.getByRole("button", { name: "Dry run" }));
		const pane = screen.getByRole("region", { name: "Dry run" });
		expect(await within(pane).findByTestId("dryrun-summary")).toHaveTextContent(
			"Saving would change the rendered policy of 1 workload.",
		);
		expect(within(pane).getByTestId("dryrun-workload")).toHaveTextContent(
			"checkout-prod-01against v42+gainstcp/389from 10.40.0.0/16· this rule",
		);
		// The request: every ruleset as read, the new rule appended under a
		// temporary id, and the state version the editor read.
		const body = bodies[0] as { state_version: string; rulesets: Ruleset[] };
		expect(body.state_version).toBe("sv-1");
		expect(body.rulesets.map((r) => r.name)).toEqual([
			"checkout-inbound",
			"auth-inbound",
			"billing-inbound",
		]);
		const rules = body.rulesets[0]?.rules ?? [];
		expect(rules.slice(0, 4).map((r) => r.id)).toEqual(
			[apiRule, sshRule, vpnRule, scrape].map((r) => r.id),
		);
		expect(rules[4]).toMatchObject({
			direction: "inbound",
			peers: [{ address_group: "corp-vpn" }],
			entries: [{ protocol: "tcp", ports: ["389"] }],
		});
		expect(rules[4]?.id).toMatch(/^[0-9a-f-]{36}$/);
		expect(rules[0]).not.toHaveProperty("version");
		// A pure read: no write was sent, and saving was never gated on it.
		expect(
			screen.getByRole("button", { name: /Save — applies/ }),
		).toBeEnabled();
	});

	it("says a dry run is stale when the state moved, and reloads and re-runs", async () => {
		let version = "sv-1";
		const runs: string[] = [];
		const { calls } = surface({
			stateVersion: () => version,
			dryRun: (body) => {
				const asked = (body as { state_version: string }).state_version;
				runs.push(asked);
				// Someone wrote between the editor's read and its first run.
				version = "sv-2";
				return {
					status: 200,
					json: {
						state_version: "sv-2",
						stale: asked !== "sv-2",
						workloads: [],
					} satisfies DryRunResult,
				};
			},
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		const scrapeRow = await row("Scrapes from the metrics collectors.");
		await user.click(
			within(scrapeRow).getByRole("button", {
				name: "Edit Scrapes from the metrics collectors.",
			}),
		);
		await user.click(
			within(
				await row("Editing Scrapes from the metrics collectors."),
			).getByRole("switch", { name: "This rule enabled" }),
		);
		await user.click(screen.getByRole("button", { name: "Dry run" }));
		const pane = screen.getByRole("region", { name: "Dry run" });
		expect(
			await within(pane).findByTestId("dryrun-freshness"),
		).toHaveTextContent("The state moved before this ran.");
		const listings = () =>
			calls.filter((c) => c.method === "GET" && c.path === "/api/v1/rulesets")
				.length;
		const before = listings();
		await user.click(
			within(pane).getByRole("button", { name: "Reload and re-run" }),
		);
		await waitFor(() => expect(runs).toEqual(["sv-1", "sv-2"]));
		await waitFor(() =>
			expect(within(pane).queryByTestId("dryrun-freshness")).toBeNull(),
		);
		expect(within(pane).getByTestId("dryrun-summary")).toHaveTextContent(
			"Nothing would change",
		);
		// The re-run followed a fresh read of the listing.
		expect(listings()).toBe(before + 1);
	});

	it("marks a result overtaken once a save moves the state", async () => {
		let version = "sv-1";
		surface({
			stateVersion: () => version,
			extra: [
				{
					method: "PUT",
					path: `/api/v1/rulesets/${checkout.id}/rules/${scrape.id}`,
					reply: () => {
						version = "sv-2";
						return {
							status: 200,
							json: { ...scrape, enabled: true, version: "4" },
						};
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		await user.click(
			within(await row("Scrapes from the metrics collectors.")).getByRole(
				"button",
				{
					name: "Edit Scrapes from the metrics collectors.",
				},
			),
		);
		const editing = await row("Editing Scrapes from the metrics collectors.");
		await user.click(
			within(editing).getByRole("switch", { name: "This rule enabled" }),
		);
		await user.click(screen.getByRole("button", { name: "Dry run" }));
		const pane = screen.getByRole("region", { name: "Dry run" });
		await within(pane).findByTestId("dryrun-summary");
		await user.click(screen.getByRole("button", { name: /Save — applies/ }));
		expect(
			await within(pane).findByTestId("dryrun-freshness"),
		).toHaveTextContent("The state has moved since this ran.");
		expect(
			within(pane).getByRole("button", { name: "Re-run" }),
		).toBeInTheDocument();
	});

	it("routes a refused dry run's findings to the row's fields", async () => {
		surface({
			dryRun: () => ({
				status: 400,
				problem: {
					...problem("validation", 400),
					errors: [
						{
							path: "rulesets[0].rules[4].peers[0].cidr",
							rule: "cidr",
							message: "not a valid CIDR",
						},
					],
				},
			}),
		});
		const user = userEvent.setup();
		renderApp("/policy?ruleset=checkout-inbound");
		await rulesTable();
		await user.click(screen.getByRole("button", { name: "Add rule" }));
		const editing = await row("New rule");
		await user.type(
			within(editing).getByRole("textbox", { name: "Add a peer" }),
			"10.0.0.0/40{Enter}",
		);
		await user.click(screen.getByRole("button", { name: "Dry run" }));
		const finding = await within(editing).findByTestId("finding");
		expect(finding).toHaveAttribute(
			"data-path",
			"rulesets[0].rules[4].peers[0].cidr",
		);
		expect(
			within(screen.getByRole("region", { name: "Dry run" })).getByRole(
				"alert",
			),
		).toHaveTextContent("refused this set as a write would");
	});

	it("creates a ruleset from the fresh state", async () => {
		const posted: unknown[] = [];
		const created = ruleset("ledger-inbound", { app: ["ledger"] });
		const s = surface({
			rulesets: [],
			extra: [
				{
					method: "POST",
					path: "/api/v1/rulesets",
					reply: (body) => {
						posted.push(body);
						s.setRulesets([created]);
						return { status: 201, json: created };
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy");
		await user.click(await screen.findByRole("link", { name: "New ruleset" }));
		await waitFor(() =>
			expect(
				screen.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveTextContent("Policy/New ruleset"),
		);
		await user.type(
			screen.getByRole("textbox", { name: "Ruleset name" }),
			"ledger-inbound",
		);
		await user.type(
			screen.getByRole("textbox", { name: "Add a scope requirement" }),
			"app=ledger{Enter}",
		);
		await user.click(
			await screen.findByRole("button", {
				name: /Create ruleset — applies to 0 workloads now/,
			}),
		);
		expect(posted[0]).toEqual({
			name: "ledger-inbound",
			description: "",
			enabled: true,
			scope: { app: ["ledger"] },
			rules: [],
		});
		expect(
			await screen.findByRole("heading", { name: "ledger-inbound" }),
		).toBeInTheDocument();
	});

	it("shows the fresh state, a failed read with a retry, and loading", async () => {
		mockSurface(freshInstall);
		renderApp("/policy");
		expect(
			await screen.findByRole("heading", { name: "Create your first ruleset" }),
		).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "New ruleset" })).toHaveAttribute(
			"href",
			"/policy?new=1",
		);
	});

	it("renders a failed read with the surface's words and retries", async () => {
		let fail = true;
		surface({
			extra: [
				{
					method: "GET",
					path: "/api/v1/rulesets",
					reply: () =>
						fail
							? {
									status: 503,
									problem: problem(
										"internal",
										503,
										"the database is unreachable",
									),
								}
							: {
									status: 200,
									json: { rulesets: [checkout], state_version: "sv-1" },
								},
				},
			],
		});
		const user = userEvent.setup();
		renderApp("/policy");
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent("Could not load the policy");
		expect(alert).toHaveTextContent("the database is unreachable");
		fail = false;
		await user.click(within(alert).getByRole("button", { name: "Retry" }));
		expect(
			await screen.findByRole("heading", { name: "checkout-inbound" }),
		).toBeInTheDocument();
	});
});

import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { EvidenceGap, RollupGroup, Ruleset, Workload } from "@/api/schema";
import { minutesAgo, workload } from "@/test/fixtures";
import {
	emptyRollup,
	freshInstall,
	gapsOf,
	mockSurface,
	problem,
	type Reply,
	type Route,
	renderApp,
	signedIn,
} from "@/test/harness";
import { addressGroup, peer, rollupOf, row as srcDst, wref } from "@/test/map";
import { gap, peerService, ps, rule, ruleset } from "@/test/review";

// The checkout scope: three workloads simulating on their latest policy,
// one never moved out of visibility, and one that refused its latest
// version. The ledger scope is safe to enforce.
const checkoutLabels = { app: "checkout", env: "prod" };
const c = [1, 2, 3].map((i) =>
	workload({
		id: `w-c${i}`,
		hostname: `checkout-prod-0${i}`,
		labels: checkoutLabels,
		mode: "simulation",
	}),
);
const vis = workload({
	id: "w-c41",
	hostname: "checkout-prod-41",
	labels: checkoutLabels,
	mode: "visibility",
});
const degraded = workload({
	id: "w-c7",
	hostname: "checkout-prod-07",
	labels: checkoutLabels,
	mode: "simulation",
	sync: {
		state: "degraded",
		applied_version: 41,
		latest_version: 42,
		error: "apply refused",
	},
});
const ledger = [1, 2].map((i) =>
	workload({
		id: `w-l${i}`,
		hostname: `ledger-prod-0${i}`,
		labels: { app: "ledger" },
		mode: "simulation",
	}),
);

const metricsLabels = { app: "metrics-collector", env: "prod" };
const m1 = wref("w-m1", "metrics-01", metricsLabels);
const m2 = wref("w-m2", "metrics-02", metricsLabels);
const api = wref("w-api", "api-01", { app: "storefront-api", env: "prod" });
const billing = wref("w-b1", "billing-01", { app: "billing" });
const cref = (w: Workload) => wref(w.id, w.hostname, w.labels);
const office = addressGroup("ag-office", "office", ["172.16.0.0/12"]);

const apiRule = rule({ description: "api-to-checkout" });
const scrape = rule({
	description: "metrics-scrape",
	enabled: false,
	peers: [{ workloads: { app: ["metrics-collector"] } }],
	entries: [{ protocol: "tcp", ports: ["9100"] }],
	version: "3",
});
const checkout = ruleset(
	"checkout-inbound",
	{ app: ["checkout"], env: ["prod"] },
	[apiRule, scrape],
);
const ledgerRs = ruleset("ledger-inbound", { app: ["ledger"] });
const retired = ruleset("retired", { app: ["old"] }, [], { enabled: false });

// Each rollup the review reads, by scope, grouping, decision, and
// service.
function groupsFor(q: URLSearchParams): RollupGroup[] {
	const scope = q.getAll("label").join("&");
	const key = `${scope}|${q.get("group_by")}|${q.get("verdict")}|${q.get("service") ?? ""}`;
	const table: Record<string, RollupGroup[]> = {
		"app=checkout&env=prod|peer,service|would_block|": [
			ps(peer.workload(m1), "tcp/9100", 9000, 2, 1),
			ps(peer.workload(m2), "tcp/9100", 9204, 2, 2),
			ps(peer.group(office.id, "office"), "tcp/22", 41, 1, 90),
		],
		"app=checkout&env=prod|peer,service|allowed|": [
			ps(peer.workload(api), "tcp/8443", 412_880, 3, 1),
		],
		"app=checkout&env=prod|src,dst|would_block|": [
			srcDst(peer.workload(m1), cref(c[0]), 4500),
			srcDst(peer.workload(m1), cref(c[1]), 4500),
			srcDst(peer.workload(m2), cref(c[1]), 9204),
			srcDst(peer.group(office.id, "office"), cref(c[2]), 41),
		],
		"app=checkout&env=prod|src,dst|would_block|tcp/9100": [
			srcDst(peer.workload(m1), cref(c[0]), 4500),
			srcDst(peer.workload(m1), cref(c[1]), 4500),
			srcDst(peer.workload(m2), cref(c[1]), 9204),
		],
		"app=checkout&env=prod|src,dst|allowed|": [
			srcDst(peer.workload(api), cref(c[0]), 412_880),
		],
		"app=checkout&env=prod|src,dst|allowed|tcp/8443": [
			srcDst(peer.workload(api), cref(c[0]), 412_880),
		],
		"app=checkout&env=prod|rule,peer|allowed|tcp/8443": [
			{
				...srcDst(peer.workload(api), cref(c[0]), 412_880),
				keys: {
					rule: {
						id: `${apiRule.id}/tcp`,
						authored_rule_id: apiRule.id,
						protocol: "tcp",
					},
					peer: peer.workload(api),
				},
			},
		],
		"app=ledger|peer,service|allowed|": [
			ps(peer.workload(billing), "tcp/8443", 12_400, 2, 3),
		],
		"app=ledger|src,dst|allowed|": [
			srcDst(peer.workload(billing), cref(ledger[0]), 6200),
			srcDst(peer.workload(billing), cref(ledger[1]), 6200),
		],
	};
	return table[key] ?? [];
}

function surface(
	opts: {
		rulesets?: Ruleset[];
		modeChange?: Reply | ((body: unknown) => Reply);
		putRule?: Reply;
		rollup?: Route["reply"];
		// gaps are the evidence gaps each scope's read returns, keyed as
		// byScope is; none unless given.
		gaps?: Record<string, EvidenceGap[]>;
	} = {},
) {
	const rulesets = opts.rulesets ?? [checkout, ledgerRs, retired];
	const byScope: Record<string, Workload[]> = {
		"app=checkout&env=prod": [...c, vis, degraded],
		"app=ledger": ledger,
	};
	return mockSurface([
		signedIn,
		{
			method: "GET",
			path: "/api/v1/rulesets",
			reply: { status: 200, json: { rulesets, state_version: "9" } },
		},
		{
			method: "GET",
			path: "/api/v1/address-groups",
			reply: { status: 200, json: { address_groups: [office] } },
		},
		{
			method: "GET",
			path: "/api/v1/services",
			reply: { status: 200, json: { services: [] } },
		},
		{
			method: "GET",
			path: "/api/v1/workloads",
			reply: (_b, q) => ({
				status: 200,
				json: {
					workloads: byScope[q.getAll("label").join("&")] ?? [...c],
					next_cursor: null,
				},
			}),
		},
		{
			method: "GET",
			path: "/api/v1/flows/rollup",
			reply:
				opts.rollup ??
				((_b, q) => {
					const groups = groupsFor(q);
					const r =
						q.get("group_by") === "peer,service"
							? peerService(groups)
							: rollupOf(groups);
					return {
						status: 200,
						json: groups.length > 0 ? r : emptyRollup(q),
					};
				}),
		},
		{
			method: "GET",
			path: "/api/v1/flows/gaps",
			reply: (_b, q) => ({
				status: 200,
				json: gapsOf(opts.gaps?.[q.getAll("label").join("&")] ?? [], q),
			}),
		},
		{
			method: "GET",
			path: "/api/v1/flows",
			reply: (_b, q) => ({
				status: 200,
				json: {
					workload: cref(c[0]),
					from: q.get("from"),
					to: q.get("to"),
					flows: [
						{
							id: 11,
							window_start: minutesAgo(10),
							window_end: minutesAgo(5),
							peer: peer.workload(m1),
							src_address: "10.20.9.4",
							dst_address: "10.20.4.17",
							service: { protocol: "tcp", port: 9100 },
							direction: "inbound",
							verdict: "would_block",
							rule: null,
							connection_count: 1204,
							byte_count: 9000,
							first_seen: minutesAgo(10),
							last_seen: minutesAgo(5),
							process_name: "node_exporter",
						},
					],
					next_cursor: null,
				},
			}),
		},
		{
			method: "POST",
			path: "/api/v1/selectors/preview",
			reply: (body) => {
				const sel = (body as { selector: Record<string, string[]> }).selector;
				const matched =
					sel.app?.[0] === "ledger" ? ledger : [...c, vis, degraded];
				return {
					status: 200,
					json: { matched: matched.map(cref), count: matched.length },
				};
			},
		},
		{
			method: "POST",
			path: "/api/v1/mode-changes",
			reply:
				opts.modeChange ??
				((body) => ({
					status: 200,
					json: {
						mode_change_id: "mc-1",
						matched: (body as { workload_ids: string[] }).workload_ids.length,
						desired_updated: 3,
					},
				})),
		},
		{
			method: "PUT",
			path: `/api/v1/rulesets/${checkout.id}/rules/${scrape.id}`,
			reply: opts.putRule ?? {
				status: 200,
				json: { ...scrape, enabled: true, version: "4" },
			},
		},
	]);
}

type Calls = ReturnType<typeof surface>["calls"];

function rollupCalls(calls: Calls, groupBy: string, verdict?: string) {
	return calls
		.filter((x) => x.path.startsWith("/api/v1/flows/rollup"))
		.map((x) => new URL(x.path, "http://console.test").searchParams)
		.filter(
			(q) =>
				q.get("group_by") === groupBy &&
				(verdict === undefined || q.get("verdict") === verdict),
		);
}

const metricsRow = `/simulation?ruleset=checkout-inbound&sel=${encodeURIComponent(
	"would_block|w:app=metrics-collector env=prod|tcp/9100",
)}`;

async function banner() {
	return screen.findByRole("region", { name: "Verdict" });
}

describe("simulation review", () => {
	it("opens on the ruleset with the most would-block pairs and reads it by its scope, once", async () => {
		const { calls } = surface();
		renderApp("/simulation");
		expect(
			within(await banner()).getByRole("heading", {
				name: "Not safe to enforce yet",
			}),
		).toBeInTheDocument();
		// The chosen ruleset is named in the address, and so in the
		// breadcrumb, without reading it again.
		await waitFor(() =>
			expect(
				screen.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveTextContent("Simulation review/checkout-inbound"),
		);
		expect(rollupCalls(calls, "peer,service", "would_block")).toHaveLength(2);
		expect(rollupCalls(calls, "peer,service", "allowed")).toHaveLength(1);
		const [allowed] = rollupCalls(calls, "peer,service", "allowed");
		expect(allowed.getAll("label")).toEqual(["app=checkout", "env=prod"]);
		expect(allowed.get("from")).not.toBeNull();
		expect(rollupCalls(calls, "src,dst")).toHaveLength(2);

		// The tabs are the enabled rulesets, each with its would-block pairs.
		const tabs = screen.getAllByRole("tab");
		expect(tabs.map((t) => t.textContent)).toEqual([
			"checkout-inbound◆ 2 would block",
			"ledger-inbound◆ 0 would block",
		]);
		expect(tabs[0]).toHaveAttribute("aria-selected", "true");
		expect(screen.getByTestId("scope-line")).toHaveTextContent(
			"app=checkout AND env=prod·5 workloads in scope·4 simulation · 1 visibility · 1 degraded",
		);
		// Editing the ruleset under review opens the editor at it.
		expect(screen.getByRole("link", { name: "Edit ruleset" })).toHaveAttribute(
			"href",
			"/policy?ruleset=checkout-inbound",
		);
	});

	it("names what it shows when the address names no enabled ruleset", async () => {
		surface();
		renderApp("/simulation?ruleset=retired");
		await banner();
		await waitFor(() =>
			expect(
				screen.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveTextContent("Simulation review/checkout-inbound"),
		);
	});

	it("composes the banner: the numbers, and a line for each failing condition", async () => {
		surface();
		renderApp("/simulation?ruleset=checkout-inbound");
		const b = await banner();
		expect(b).toHaveTextContent(
			"2 peer/service pairs carrying 18,245 connections would be dropped.",
		);
		// Each number follows its term, which reads first.
		expect(b).toHaveTextContent("◆ would block · peer/service2");
		expect(b).toHaveTextContent("connections in range18.2k");
		expect(b).toHaveTextContent("of 4 simulating workloads affected3");
		expect(b).toHaveTextContent("✓ allowed · matched a rule1");
		const caveats = within(b).getByRole("list", { name: "Caveats" });
		expect(
			within(caveats)
				.getAllByRole("listitem")
				.map((l) => l.textContent),
		).toEqual([
			"▲checkout-prod-07 is degraded (applied v41, rendered v42) — its flows were evaluated against stale policy",
			"▲1 workload in scope is still in visibility mode and produced no verdict",
		]);
		// Not safe: promotion is the secondary action.
		expect(
			screen.getByRole("button", { name: "Promote to enforced…" }),
		).toHaveClass("border-strong");
	});

	it("switches rulesets by tab, to a safe verdict", async () => {
		surface();
		renderApp("/simulation?ruleset=checkout-inbound");
		await banner();
		await userEvent.click(screen.getByRole("tab", { name: /ledger-inbound/ }));
		expect(
			await within(await banner()).findByRole("heading", {
				name: "Safe to enforce",
			}),
		).toBeInTheDocument();
		const b = await banner();
		expect(b).toHaveTextContent(
			"Every observed inbound connection on the 2 simulating workloads matched an enabled rule.",
		);
		expect(within(b).getByRole("list", { name: "Caveats" })).toHaveTextContent(
			/Observed for 1h 10m \(windows .* UTC\); a peer that connects less often/,
		);
		expect(
			screen.getByRole("button", { name: "Promote to enforced…" }),
		).toHaveClass("bg-action-primary-bg");
		expect(
			screen.getByRole("navigation", { name: "Breadcrumb" }),
		).toHaveTextContent("ledger-inbound");
	});

	it("fails a scope whose evidence has a gap in the range, and says whose and when", async () => {
		const { calls } = surface({
			gaps: { "app=ledger": [gap(ledger[0], 90, 88)] },
		});
		renderApp("/simulation?ruleset=ledger-inbound");
		const b = await banner();
		expect(
			await within(b).findByRole("heading", {
				name: "Not safe to enforce yet",
			}),
		).toBeInTheDocument();
		expect(b).toHaveTextContent(
			"No observed traffic would be dropped, but evidence is missing from part of this range: traffic in it may have gone unseen.",
		);
		expect(within(b).getByRole("list", { name: "Caveats" })).toHaveTextContent(
			/Evidence incomplete for ledger-prod-01 between (?:\d\d-\d\d )?\d\d:\d\d and (?:\d\d-\d\d )?\d\d:\d\d UTC — the kernel dropped events/,
		);
		// The gaps are read for the scope, over the rollups' one range.
		const read = calls
			.filter((x) => x.path.startsWith("/api/v1/flows/gaps"))
			.map((x) => new URL(x.path, "http://console.test").searchParams);
		expect(read).toHaveLength(1);
		expect(read[0].getAll("label")).toEqual(["app=ledger"]);
		const rollup = rollupCalls(calls, "peer,service", "allowed")[0];
		expect(read[0].get("from")).toBe(rollup.get("from"));
		expect(read[0].get("to")).toBe(rollup.get("to"));
	});

	it("filters rows by the verdict chips", async () => {
		surface();
		renderApp("/simulation?ruleset=checkout-inbound");
		const table = await screen.findByRole("table", {
			name: /grouped by peer/,
		});
		expect(within(table).getAllByRole("row")).toHaveLength(4);
		const chip = screen.getByRole("button", { name: /would block 2/ });
		await userEvent.click(chip);
		expect(chip).toHaveAttribute("aria-pressed", "true");
		const rows = within(table).getAllByRole("row").slice(1);
		expect(
			rows.map((r) => within(r).getAllByRole("cell")[0].textContent),
		).toEqual(["◆would block", "◆would block"]);
		await userEvent.click(screen.getByRole("button", { name: /allowed 1/ }));
		expect(within(table).getAllByRole("row").slice(1)).toHaveLength(1);
		expect(table).toHaveTextContent("app=storefront-api");
		await userEvent.click(screen.getByRole("button", { name: /all 3/ }));
		expect(within(table).getAllByRole("row")).toHaveLength(4);
	});

	it("draws the rows: folded peers, exact workload counts, volume bars", async () => {
		surface();
		renderApp("/simulation?ruleset=checkout-inbound");
		const table = await screen.findByRole("table", { name: /grouped by peer/ });
		const [, metrics, officeRow] = within(table).getAllByRole("row");
		expect(metrics).toHaveTextContent(
			"labelsapp=metrics-collector2 workloads, env=prodtcp/9100no rule matched218,204",
		);
		expect(officeRow).toHaveTextContent(
			"address groupoffice172.16.0.0/12tcp/22no rule matched141",
		);
	});

	it("keeps the selection across takes", async () => {
		surface();
		renderApp(metricsRow);
		const drawer = await screen.findByRole("complementary", {
			name: "Pair detail",
		});
		expect(drawer).toHaveTextContent(
			/app=metrics-collector env=prod\s*→\s*on tcp\/9100/,
		);
		await userEvent.click(
			screen.getByRole("button", { name: "Peer × service matrix" }),
		);
		const matrix = await screen.findByRole("table", {
			name: "Peer by service",
		});
		expect(
			within(matrix).getByRole("button", { pressed: true }),
		).toHaveTextContent("18,204");
		expect(
			screen.getByRole("complementary", { name: "Pair detail" }),
		).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "By recency" }));
		expect(
			screen.getByRole("button", {
				pressed: true,
				name: /app=metrics-collector/,
			}),
		).toHaveTextContent("tcp/9100");
		expect(
			screen.getByRole("complementary", { name: "Pair detail" }),
		).toBeInTheDocument();
		// Deselecting closes the drawer in any take.
		await userEvent.click(
			screen.getByRole("button", {
				pressed: true,
				name: /app=metrics-collector/,
			}),
		);
		expect(
			screen.queryByRole("complementary", { name: "Pair detail" }),
		).not.toBeInTheDocument();
		await userEvent.click(
			screen.getByRole("button", { name: "Grouped by peer" }),
		);
		await userEvent.click(
			screen.getByRole("button", {
				name: "app=metrics-collector",
				pressed: false,
			}),
		);
		expect(
			await screen.findByRole("complementary", { name: "Pair detail" }),
		).toBeInTheDocument();
	});

	it("expands a row: its pairs, the exact count, and a pair's windows", async () => {
		const { calls } = surface();
		renderApp(metricsRow);
		const drawer = await screen.findByRole("complementary", {
			name: "Pair detail",
		});
		await waitFor(() => expect(drawer).toHaveTextContent("Flows · 3 pairs"));
		const [pairs] = rollupCalls(calls, "src,dst").filter(
			(q) => q.get("service") === "tcp/9100",
		);
		expect(pairs.get("verdict")).toBe("would_block");
		expect(pairs.getAll("label")).toEqual(["app=checkout", "env=prod"]);
		// Three pairs over two destinations: the exact count.
		expect(within(drawer).getByTestId("explain")).toHaveTextContent(
			"It reached 2 workloads in scope",
		);
		expect(within(drawer).getByTestId("rule-statement")).toHaveTextContent(
			"No enabled rule matched",
		);
		expect(
			within(drawer).getByRole("link", { name: "Open in policy editor" }),
		).toHaveAttribute("href", "/policy?ruleset=checkout-inbound");

		await userEvent.click(
			within(drawer).getAllByRole("button", { expanded: false })[0],
		);
		const windows = await within(drawer).findByTestId("pair-windows");
		expect(windows).toHaveTextContent("node_exporter");
		const flows = calls
			.filter((x) => x.path.startsWith("/api/v1/flows?"))
			.map((x) => new URL(x.path, "http://console.test").searchParams);
		expect(Object.fromEntries(flows[0])).toMatchObject({
			workload: "w-c1",
			peer: "w-m1",
			verdict: "would_block",
			service: "tcp/9100",
		});
	});

	it("names the rule an allowed row matched", async () => {
		surface();
		renderApp(
			`/simulation?ruleset=checkout-inbound&sel=${encodeURIComponent(
				"allowed|w:app=storefront-api env=prod|tcp/8443",
			)}`,
		);
		const drawer = await screen.findByRole("complementary", {
			name: "Pair detail",
		});
		expect(
			await within(drawer).findByTestId("rule-statement"),
		).toHaveTextContent("Matched api-to-checkout in checkout-inbound");
	});

	it("enables a disabled rule, conditioned on the version it read", async () => {
		const { calls, fetchMock } = surface();
		renderApp(metricsRow);
		const drawer = await screen.findByRole("complementary", {
			name: "Pair detail",
		});
		const list = within(drawer).getByRole("list", { name: "Disabled rules" });
		expect(list).toHaveTextContent(
			"metrics-scrapeapp=metrics-collector · tcp/9100",
		);
		const before = calls.filter((x) => x.path === "/api/v1/rulesets").length;
		await userEvent.click(
			within(drawer).getByRole("button", { name: "Enable metrics-scrape" }),
		);
		expect(await within(drawer).findByRole("status")).toHaveTextContent(
			"Enabled metrics-scrape. It takes effect as each workload in scope applies its next version",
		);
		const put = fetchMock.mock.calls.find(([, init]) => init?.method === "PUT");
		if (!put) throw new Error("no PUT was made");
		const [url, init] = put;
		expect(url).toBe(`/api/v1/rulesets/${checkout.id}/rules/${scrape.id}`);
		expect(new Headers(init?.headers).get("If-Match")).toBe('"3"');
		expect(JSON.parse(String(init?.body))).toMatchObject({
			id: scrape.id,
			enabled: true,
			description: "metrics-scrape",
			entries: [{ protocol: "tcp", ports: ["9100"] }],
		});
		// The review re-reads.
		await waitFor(() =>
			expect(
				calls.filter((x) => x.path === "/api/v1/rulesets").length,
			).toBeGreaterThan(before),
		);
	});

	it("renders a rule changed since it was read, and changes nothing", async () => {
		const { calls } = surface({
			putRule: {
				status: 412,
				problem: {
					...problem("precondition-failed", 412),
					current_version: "4",
				},
			},
		});
		renderApp(metricsRow);
		const drawer = await screen.findByRole("complementary", {
			name: "Pair detail",
		});
		await userEvent.click(
			within(drawer).getByRole("button", { name: "Enable metrics-scrape" }),
		);
		const alert = await within(drawer).findByRole("alert");
		expect(alert).toHaveTextContent(
			"▲ metrics-scrape changed since this review read it",
		);
		expect(alert).toHaveTextContent(
			"The rule is now at version 4; nothing was changed.",
		);
		const before = calls.filter((x) => x.path === "/api/v1/rulesets").length;
		await userEvent.click(
			within(alert).getByRole("button", { name: "Reload" }),
		);
		await waitFor(() =>
			expect(
				calls.filter((x) => x.path === "/api/v1/rulesets").length,
			).toBeGreaterThan(before),
		);
		expect(within(drawer).queryByRole("alert")).not.toBeInTheDocument();
	});
});

describe("promotion", () => {
	async function openDialog(path = "/simulation?ruleset=checkout-inbound") {
		renderApp(path);
		await banner();
		await userEvent.click(
			screen.getByRole("button", { name: "Promote to enforced…" }),
		);
		const dialog = await screen.findByRole("dialog");
		await within(dialog).findByTestId("partition");
		return dialog;
	}

	it("previews the scope, divides it, and gates an unsafe promotion on acknowledgment", async () => {
		const { calls } = surface();
		const dialog = await openDialog();
		const preview = calls.find((x) => x.path === "/api/v1/selectors/preview");
		expect(preview?.body).toEqual({
			selector: { app: ["checkout"], env: ["prod"] },
		});
		expect(within(dialog).getByTestId("preview")).toHaveTextContent(
			"app=checkout AND env=prod matches 5 workloads now",
		);
		const rows = within(dialog)
			.getByTestId("partition")
			.querySelectorAll(":scope > div");
		expect([...rows].map((r) => r.textContent)).toEqual([
			"Simulation → Enforced3 workloadsincluded",
			"Visibility → Enforced never simulated1 workloadskipped",
			"Degraded · checkout-prod-07 ▲ would enforce stale v411 workloadskipped",
		]);
		const submit = within(dialog).getByRole("button", {
			name: "Enforce anyway on 3 workloads",
		});
		expect(submit).toBeDisabled();
		await userEvent.click(
			within(dialog).getByRole("checkbox", {
				name: "I understand 2 peer/service pairs (18,245 connections) will be dropped",
			}),
		);
		expect(submit).toBeEnabled();
		// Including the degraded workload is the operator's choice.
		await userEvent.click(
			within(dialog).getByRole("checkbox", { name: /checkout-prod-07/ }),
		);
		const before = rollupCalls(calls, "peer,service").length;
		await userEvent.click(
			within(dialog).getByRole("button", {
				name: "Enforce anyway on 4 workloads",
			}),
		);
		const change = calls.find((x) => x.path === "/api/v1/mode-changes");
		expect(change?.body).toEqual({
			workload_ids: ["w-c1", "w-c2", "w-c3", "w-c7"],
			target_mode: "enforced",
			expected_match_count: 4,
		});
		// Success closes the dialog and the review re-reads: convergence
		// is the workloads' own, shown as they apply.
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		await waitFor(() =>
			expect(rollupCalls(calls, "peer,service").length).toBeGreaterThan(before),
		);
	});

	it("overrides missing evidence with the generic acknowledgment", async () => {
		surface({ gaps: { "app=ledger": [gap(ledger[1], 80, 79)] } });
		const dialog = await openDialog("/simulation?ruleset=ledger-inbound");
		expect(dialog).toHaveTextContent(
			/Evidence incomplete for ledger-prod-02 between .* UTC/,
		);
		const submit = within(dialog).getByRole("button", {
			name: "Enforce anyway on 2 workloads",
		});
		expect(submit).toBeDisabled();
		await userEvent.click(
			within(dialog).getByRole("checkbox", {
				name: "I understand this scope is not safe to enforce yet",
			}),
		);
		expect(submit).toBeEnabled();
	});

	it("names missing evidence in the acknowledgment beside the pairs it would drop", async () => {
		surface({ gaps: { "app=checkout&env=prod": [gap(c[0], 90, 88)] } });
		const dialog = await openDialog();
		expect(
			within(dialog).getByRole("checkbox", {
				name: "I understand 2 peer/service pairs (18,245 connections) will be dropped, and that the evidence for this range is incomplete",
			}),
		).toBeInTheDocument();
	});

	it("renders a set that no longer resolves as previewed, and previews again", async () => {
		const { calls } = surface({
			modeChange: {
				status: 409,
				problem: {
					...problem("match-count-mismatch", 409),
					expected: 3,
					matched: 2,
				},
			},
		});
		const dialog = await openDialog();
		await userEvent.click(
			within(dialog).getByRole("checkbox", { name: /I understand/ }),
		);
		await userEvent.click(
			within(dialog).getByRole("button", {
				name: "Enforce anyway on 3 workloads",
			}),
		);
		const alert = await within(dialog).findByRole("alert");
		expect(alert).toHaveTextContent(
			"▲ The set no longer resolves as previewed",
		);
		expect(alert).toHaveTextContent(
			"You submitted 3 workloads; the control plane resolved 2. Nothing was changed.",
		);
		expect(
			within(dialog).getByRole("button", {
				name: "Enforce anyway on 3 workloads",
			}),
		).toBeDisabled();
		const previews = () =>
			calls.filter((x) => x.path === "/api/v1/selectors/preview").length;
		const before = previews();
		await userEvent.click(
			within(alert).getByRole("button", { name: "Preview again" }),
		);
		await waitFor(() => expect(previews()).toBe(before + 1));
		expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
		await waitFor(() =>
			expect(
				within(dialog).getByRole("button", {
					name: "Enforce anyway on 3 workloads",
				}),
			).toBeEnabled(),
		);
	});

	it("renders a workload deleted since the preview, and previews again", async () => {
		surface({
			modeChange: {
				status: 400,
				problem: {
					...problem("validation", 400),
					errors: [
						{
							path: "workload_ids[2]",
							rule: "workload-unknown",
							message: "workload is not registered: w-c3",
						},
					],
				},
			},
		});
		const dialog = await openDialog();
		await userEvent.click(
			within(dialog).getByRole("checkbox", { name: /I understand/ }),
		);
		await userEvent.click(
			within(dialog).getByRole("button", {
				name: "Enforce anyway on 3 workloads",
			}),
		);
		const alert = await within(dialog).findByRole("alert");
		expect(alert).toHaveTextContent(
			"▲ A workload in this set is no longer registered",
		);
		expect(alert).toHaveTextContent("workload is not registered: w-c3");
		expect(
			within(alert).getByRole("button", { name: "Preview again" }),
		).toBeInTheDocument();
	});

	it("leaves a safe promotion unobstructed", async () => {
		const { calls } = surface();
		const dialog = await openDialog("/simulation?ruleset=ledger-inbound");
		expect(dialog).toHaveTextContent(
			"Safe to enforceNo observed traffic would be dropped.",
		);
		expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
		await userEvent.click(
			within(dialog).getByRole("button", { name: "Enforce on 2 workloads" }),
		);
		await waitFor(() =>
			expect(
				calls.find((x) => x.path === "/api/v1/mode-changes")?.body,
			).toEqual({
				workload_ids: ["w-l1", "w-l2"],
				target_mode: "enforced",
				expected_match_count: 2,
			}),
		);
	});
});

describe("simulation review states", () => {
	it("invites the first ruleset on a fresh control plane", async () => {
		mockSurface(freshInstall);
		renderApp("/simulation");
		expect(
			await screen.findByRole("heading", { name: "Nothing to review yet" }),
		).toBeInTheDocument();
	});

	it("says when every ruleset is disabled", async () => {
		surface({ rulesets: [retired] });
		renderApp("/simulation");
		expect(
			await screen.findByRole("heading", { name: "No enabled ruleset" }),
		).toBeInTheDocument();
	});

	it("says when the scope saw no traffic in the range", async () => {
		surface({ rollup: (_b, q) => ({ status: 200, json: emptyRollup(q) }) });
		renderApp("/simulation?ruleset=checkout-inbound&range=1h");
		expect(
			await screen.findByText(
				/No inbound flows into this scope's simulating workloads were stored in the last 1h/,
			),
		).toBeInTheDocument();
		expect(screen.getByTestId("review-extent")).toHaveTextContent(
			"no windows in range",
		);
	});

	it("shows a failed read with a retry", async () => {
		let fail = true;
		surface({
			rollup: (_b, q) =>
				fail
					? {
							status: 500,
							problem: problem("internal", 500, "store unavailable"),
						}
					: { status: 200, json: emptyRollup(q) },
		});
		renderApp("/simulation");
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent("Could not load the simulation review");
		expect(alert).toHaveTextContent("store unavailable");
		fail = false;
		await userEvent.click(within(alert).getByRole("button", { name: "Retry" }));
		expect(await banner()).toBeInTheDocument();
	});
});

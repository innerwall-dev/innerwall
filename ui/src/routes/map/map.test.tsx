import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import type { Rollup, Verdict, Workload } from "@/api/schema";
import { minutesAgo, workload } from "@/test/fixtures";
import {
	emptyRollup,
	freshInstall,
	mockSurface,
	problem,
	type Route,
	renderApp,
	signedIn,
} from "@/test/harness";
import {
	addressGroup,
	type ByVerdict,
	peer,
	rollupOf,
	row,
	wref,
} from "@/test/map";

// The graph library sizes its canvas from the layout box, which the test
// DOM does not compute; give every element the canvas's size so the map
// lays out and mounts what is in view, as a browser would.
beforeAll(() => {
	for (const [prop, value] of [
		["offsetWidth", 1200],
		["offsetHeight", 800],
		["clientWidth", 1200],
		["clientHeight", 800],
	] as const) {
		Object.defineProperty(HTMLElement.prototype, prop, {
			configurable: true,
			get: () => value,
		});
	}
});

const checkout1 = wref("w-checkout-1", "checkout-prod-01", {
	app: "checkout",
	env: "prod",
});
const metrics1 = wref("w-metrics-1", "metrics-01", {
	app: "metrics-collector",
	env: "prod",
});
const api1 = wref("w-api-1", "storefront-api-01", {
	app: "storefront-api",
	env: "prod",
});
const auth1 = wref("w-auth-1", "auth-prod-01", { app: "auth", env: "prod" });
const office = addressGroup("ag-office", "office", ["172.16.0.0/12"]);

const estate: ByVerdict = {
	would_block: [
		row(peer.workload(metrics1), checkout1, 18_204, 1),
		row(peer.group(office.id, "office"), checkout1, 41),
	],
	allowed: [
		row(peer.workload(api1), checkout1, 412_880),
		row(peer.workload(checkout1), auth1, 91_000),
	],
	blocked: [row(peer.address("198.51.100.7"), auth1, 9)],
	observed: [row(peer.workload(api1), auth1, 30)],
};

const fleet: Workload[] = [
	workload({
		id: checkout1.id,
		hostname: checkout1.hostname,
		labels: checkout1.labels,
		mode: "simulation",
	}),
	workload({
		id: auth1.id,
		hostname: auth1.hostname,
		labels: auth1.labels,
		mode: "enforced",
	}),
	workload({
		id: api1.id,
		hostname: api1.hostname,
		labels: api1.labels,
	}),
	workload({
		id: metrics1.id,
		hostname: metrics1.hostname,
		labels: metrics1.labels,
	}),
];

function surface(
	opts: {
		byVerdict?: ByVerdict;
		workloads?: Workload[];
		truncated?: boolean;
		extra?: Route[];
		rollupReply?: Route["reply"];
	} = {},
) {
	const byVerdict = opts.byVerdict ?? estate;
	const workloads = opts.workloads ?? fleet;
	return mockSurface([
		...(opts.extra ?? []),
		signedIn,
		{
			method: "GET",
			path: "/api/v1/rulesets",
			reply: { status: 200, json: { rulesets: [], state_version: "0" } },
		},
		{
			method: "GET",
			path: "/api/v1/workloads",
			reply: (_b, q) => {
				// The walk is one page at a time; this fleet is two.
				const page = q.get("cursor") === "2" ? 1 : 0;
				const half = Math.ceil(workloads.length / 2);
				const rows =
					q.get("limit") === "500"
						? page === 0
							? workloads.slice(0, half)
							: workloads.slice(half)
						: workloads.slice(0, 1);
				return {
					status: 200,
					json: {
						workloads: rows,
						next_cursor:
							q.get("limit") === "500" && page === 0 && half < workloads.length
								? "2"
								: null,
					},
				};
			},
		},
		{
			method: "GET",
			path: "/api/v1/address-groups",
			reply: { status: 200, json: { address_groups: [office] } },
		},
		{
			method: "GET",
			path: "/api/v1/flows/rollup",
			reply:
				opts.rollupReply ??
				((_b, q) => {
					const v = q.get("verdict") as Verdict | null;
					if (q.get("group_by") !== "src,dst" || !v) {
						return { status: 200, json: emptyRollup(q) };
					}
					const r: Rollup = rollupOf(byVerdict[v] ?? [], {
						truncated: opts.truncated && v === "observed",
						from: q.get("from") ?? undefined,
						to: q.get("to") ?? undefined,
					});
					return { status: 200, json: r };
				}),
		},
	]);
}

async function openMap(path = "/map") {
	renderApp(path);
	return screen.findByText("connections");
}

describe("flow map", () => {
	it("reads the rollup once per decision over one range, and walks the workloads in scope", async () => {
		const { calls } = surface();
		await openMap("/map?label=env%3Dprod");
		const rollups = calls
			.filter((c) => c.path.startsWith("/api/v1/flows/rollup"))
			.map((c) => new URL(c.path, "http://x").searchParams);
		expect(rollups.map((q) => q.get("verdict")).sort()).toEqual([
			"allowed",
			"blocked",
			"observed",
			"would_block",
		]);
		for (const q of rollups) {
			expect(q.get("group_by")).toBe("src,dst");
			expect(q.getAll("label")).toEqual(["env=prod"]);
			expect(q.get("from")).toBe(rollups[0].get("from"));
			expect(q.get("to")).toBe(rollups[0].get("to"));
			expect(q.get("limit")).toBe("1000");
		}
		const walk = calls
			.filter((c) => c.path.startsWith("/api/v1/workloads?"))
			.map((c) => new URL(c.path, "http://x").searchParams)
			.filter((q) => q.get("limit") === "500");
		expect(walk.map((q) => q.get("cursor"))).toEqual([null, "2"]);
		expect(walk[0].getAll("label")).toEqual(["env=prod"]);
		// The scope names the screen.
		expect(
			screen.getByRole("navigation", { name: "Breadcrumb" }),
		).toHaveTextContent("Flow map/env=prod");
	});

	it("draws label groups with their members, modes, and unmanaged peers", async () => {
		surface();
		await openMap();
		const graph = await screen.findByTestId("flow-graph");
		const checkout = await within(graph).findByRole("button", {
			name: "checkout, 1 workload",
		});
		expect(checkout).toHaveTextContent("simulation");
		expect(
			within(graph).getByRole("button", { name: "auth, 1 workload" }),
		).toHaveTextContent("enforced");
		expect(
			within(graph).getByRole("button", { name: "office, 172.16.0.0/12" }),
		).toHaveTextContent("unmanaged · address group");
		// An address no group holds is drawn as the unknown peers, never as
		// a node of its own.
		expect(
			within(graph).getByRole("button", { name: "unknown peers, 1 address" }),
		).toHaveTextContent("unmanaged · no address group");
		expect(
			within(graph).queryByRole("button", { name: /^198\.51\.100\.7/ }),
		).not.toBeInTheDocument();
		expect(screen.getByTestId("map-legend")).toBeInTheDocument();
		// 18,204 + 41 + 412,880 + 91,000 + 9 + 30 connections, into two
		// workloads.
		expect(screen.getByText("522.2k")).toBeInTheDocument();
		expect(screen.getByText("workloads reporting")).toHaveTextContent("2");
	});

	it("opens an edge's decisions and pairs, and a pair's windows from GET /flows", async () => {
		const flows: URLSearchParams[] = [];
		surface({
			extra: [
				{
					method: "GET",
					path: "/api/v1/flows",
					reply: (_b, q) => {
						flows.push(q);
						return {
							status: 200,
							json: {
								workload: checkout1,
								from: q.get("from"),
								to: q.get("to"),
								next_cursor: null,
								flows: [
									{
										id: 7,
										window_start: minutesAgo(10),
										window_end: minutesAgo(5),
										peer: peer.workload(metrics1),
										src_address: "10.64.2.1",
										dst_address: "10.64.0.1",
										service: { protocol: "tcp", port: 9100 },
										direction: "inbound",
										verdict: "would_block",
										rule: null,
										connection_count: 18_204,
										byte_count: 900,
										first_seen: minutesAgo(10),
										last_seen: minutesAgo(1),
									},
								],
							},
						};
					},
				},
			],
		});
		const user = userEvent.setup();
		await openMap("/map?label=env%3Dprod");
		const graph = await screen.findByTestId("flow-graph");
		const chip = await within(graph).findByRole("button", {
			name: "metrics-collector to checkout: would block 18204 connections",
		});
		await user.click(chip);

		const drawer = await screen.findByRole("complementary", {
			name: "Traffic between groups",
		});
		expect(drawer).toHaveTextContent("metrics-collector→tocheckout");
		expect(drawer).toHaveTextContent("would block18,204");
		expect(drawer).toHaveTextContent(
			"checkout is simulating. This traffic still flows, but the current ruleset would drop it if enforced.",
		);
		expect(within(drawer).getByTestId("rule-peers")).toHaveTextContent(
			"app=metrics-collector AND env=prod",
		);
		expect(
			within(drawer).getByRole("button", { name: "Open in policy editor" }),
		).toHaveAttribute("aria-disabled", "true");
		// One pair: its windows open at once, by workload, peer, and decision
		// over the map's own range.
		expect(await within(drawer).findByTestId("pair-flows")).toHaveTextContent(
			"tcp/9100",
		);
		expect(flows).toHaveLength(1);
		expect(flows[0].get("workload")).toBe(checkout1.id);
		expect(flows[0].get("peer")).toBe(metrics1.id);
		expect(flows[0].get("verdict")).toBe("would_block");
		expect(flows[0].get("from")).not.toBeNull();
		expect(flows[0].get("to")).not.toBeNull();
		expect(within(drawer).getByTestId("rule-services")).toHaveTextContent(
			"tcp/9100",
		);
		expect(
			within(drawer).getByRole("link", { name: "Open checkout-prod-01" }),
		).toHaveAttribute("href", `/workloads/${checkout1.id}`);
		expect(chip).toHaveAttribute("aria-pressed", "true");

		// Closing clears the selection.
		await user.click(within(drawer).getByRole("button", { name: "Close" }));
		expect(
			screen.queryByRole("complementary", { name: "Traffic between groups" }),
		).not.toBeInTheDocument();
	});

	it("scopes to a selected node and links its workloads to their detail", async () => {
		surface();
		const user = userEvent.setup();
		await openMap();
		const graph = await screen.findByTestId("flow-graph");
		await user.click(
			await within(graph).findByRole("button", {
				name: "checkout, 1 workload",
			}),
		);
		const drawer = await screen.findByRole("complementary", { name: "Group" });
		expect(drawer).toHaveTextContent("Label group");
		expect(drawer).toHaveTextContent("app=checkout");
		expect(drawer).toHaveTextContent("Inbound · 3");
		expect(drawer).toHaveTextContent("Outbound · 1");
		expect(
			within(drawer).getByRole("link", { name: "checkout-prod-01" }),
		).toHaveAttribute("href", `/workloads/${checkout1.id}`);
		// From a node to one of its edges.
		await user.click(within(drawer).getByRole("button", { name: /office/ }));
		expect(
			await screen.findByRole("complementary", {
				name: "Traffic between groups",
			}),
		).toHaveTextContent("office→tocheckout");
	});

	it("keeps the selection across takes, and selects from the matrix too", async () => {
		surface();
		const user = userEvent.setup();
		await openMap();
		const graph = await screen.findByTestId("flow-graph");
		await user.click(
			await within(graph).findByRole("button", {
				name: /^storefront-api to checkout/,
			}),
		);
		await screen.findByRole("complementary", {
			name: "Traffic between groups",
		});

		await user.click(screen.getByRole("button", { name: "Matrix" }));
		const matrix = await screen.findByTestId("flow-matrix");
		const cell = within(matrix).getByRole("button", {
			name: "storefront-api to checkout: allowed 412880 connections",
		});
		expect(cell).toHaveAttribute("aria-pressed", "true");
		expect(
			screen.getByRole("complementary", { name: "Traffic between groups" }),
		).toHaveTextContent("storefront-api→tocheckout");

		// Another cell selects its edge; the same cell again clears it.
		const blocked = within(matrix).getByRole("button", {
			name: "unknown peers to auth: blocked 9 connections",
		});
		await user.click(blocked);
		expect(blocked).toHaveAttribute("aria-pressed", "true");
		expect(
			screen.getByRole("complementary", { name: "Traffic between groups" }),
		).toHaveTextContent(
			"auth is enforced. These connections were dropped on the host.",
		);
		await user.click(blocked);
		expect(
			screen.queryByRole("complementary", { name: "Traffic between groups" }),
		).not.toBeInTheDocument();

		// A node selected from a header maps back onto the graph.
		await user.click(within(matrix).getByRole("button", { name: "auth" }));
		await user.click(screen.getByRole("button", { name: "Graph" }));
		const back = await screen.findByTestId("flow-graph");
		expect(
			await within(back).findByRole("button", { name: "auth, 1 workload" }),
		).toHaveAttribute("aria-pressed", "true");
		expect(
			screen.getByRole("complementary", { name: "Group" }),
		).toHaveTextContent("Label group");
	});

	it("warns that the map is incomplete when an agent dropped flow records", async () => {
		surface({
			workloads: [
				...fleet.slice(0, 3),
				workload({
					id: metrics1.id,
					hostname: metrics1.hostname,
					labels: metrics1.labels,
					health: { dropped_flow_records: 212 },
				}),
			],
		});
		await openMap();
		expect(
			await screen.findByText(
				/1 workload dropped flow records — map may be incomplete/,
			),
		).toBeInTheDocument();
	});

	it("does not warn when no agent dropped records and nothing was cut", async () => {
		surface();
		await openMap();
		await screen.findByTestId("flow-graph");
		expect(screen.queryByText(/may be incomplete/)).not.toBeInTheDocument();
		expect(screen.queryByText(/map is incomplete/)).not.toBeInTheDocument();
	});

	it("warns when a rollup was cut at its limit", async () => {
		surface({ truncated: true });
		await openMap();
		expect(
			await screen.findByText(
				/busiest 1,000 pairs per decision — map is incomplete/,
			),
		).toBeInTheDocument();
		expect(screen.getByText("2+")).toBeInTheDocument();
	});

	it("states the windows the range actually covers", async () => {
		surface({
			rollupReply: (_b, q) => ({
				status: 200,
				json: rollupOf(
					q.get("verdict") === "allowed"
						? [row(peer.workload(api1), checkout1, 5)]
						: [],
					{
						effective:
							q.get("verdict") === "allowed"
								? ["2026-09-25T18:10:40Z", "2026-09-25T19:15:40Z"]
								: null,
					},
				),
			}),
		});
		const user = userEvent.setup();
		await openMap();
		expect(await screen.findByTestId("map-extent")).toHaveTextContent(
			"windows 18:10 → 19:15 UTC",
		);
		await user.selectOptions(screen.getByLabelText("Time range"), "7d");
		await waitFor(() =>
			expect(screen.getByLabelText("Time range")).toHaveValue("7d"),
		);
	});

	it("regroups by another key", async () => {
		surface();
		const user = userEvent.setup();
		await openMap();
		await screen.findByTestId("flow-graph");
		await user.selectOptions(screen.getByLabelText("Group by"), "env");
		const graph = await screen.findByTestId("flow-graph");
		expect(
			await within(graph).findByRole("button", { name: "prod, 4 workloads" }),
		).toBeInTheDocument();
	});

	it("adds and removes a scope requirement", async () => {
		const { calls } = surface();
		const user = userEvent.setup();
		await openMap();
		await user.click(screen.getByRole("button", { name: "+ filter" }));
		await user.type(screen.getByLabelText("Label requirement"), "bogus{Enter}");
		expect(
			screen.getByText("A label is written key=value."),
		).toBeInTheDocument();
		await user.clear(screen.getByLabelText("Label requirement"));
		await user.type(
			screen.getByLabelText("Label requirement"),
			"env=prod{Enter}",
		);
		await waitFor(() =>
			expect(
				calls.some(
					(c) =>
						c.path.includes("flows/rollup") &&
						c.path.includes("label=env%3Dprod"),
				),
			).toBe(true),
		);
		await user.click(
			await screen.findByRole("button", { name: "Remove env=prod" }),
		);
		await waitFor(() =>
			expect(
				screen.queryByRole("button", { name: "Remove env=prod" }),
			).not.toBeInTheDocument(),
		);
	});

	it("says when a scoped range has no flows", async () => {
		surface({ byVerdict: {}, workloads: [] });
		await openMap("/map?label=app%3Dsearch");
		expect(
			await screen.findByRole("heading", { name: "No flows in this range" }),
		).toBeInTheDocument();
		expect(screen.getByText(/matching app=search/)).toBeInTheDocument();
	});

	it("shows the fresh-install state on an empty control plane", async () => {
		mockSurface(freshInstall);
		renderApp("/map");
		expect(
			await screen.findByRole("heading", { name: "No flows observed yet" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: "Open enrollment" }),
		).toHaveAttribute("href", "/workloads");
	});

	it("renders a failed read with the surface's words and retries", async () => {
		let fail = true;
		surface({
			rollupReply: (_b, q) =>
				fail
					? {
							status: 400,
							problem: problem("invalid-parameter", 400, "from is after to"),
						}
					: {
							status: 200,
							json: rollupOf([], { from: q.get("from") ?? undefined }),
						},
		});
		const user = userEvent.setup();
		renderApp("/map");
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"from is after to",
		);
		fail = false;
		await user.click(screen.getByRole("button", { name: "Retry" }));
		expect(
			await screen.findByRole("heading", { name: "No flows in this range" }),
		).toBeInTheDocument();
	});
});
